package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"clovergo/internal/domain"
	"clovergo/internal/prompts"
)

const runPurposeAnswer = "answer_question"

// Ask answers a question from confirmed knowledge across the whole library.
// The answer language is fixed to Chinese by the prompt, so there is nothing
// for the caller to choose.
func (s *Service) Ask(ctx context.Context, question string) (answer domain.Answer, returnErr error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return domain.Answer{}, appError(CodeInvalidInput, "question is required", nil)
	}
	if utf8.RuneCountInString(question) > 500 {
		return domain.Answer{}, appError(CodeInvalidInput, "question must not exceed 500 characters", nil)
	}
	runID, err := newRunID()
	if err != nil {
		return domain.Answer{}, appError(CodeInternal, "generate agent run id failed", err)
	}
	runStarted := time.Now()
	if err := s.repo.CreateAgentRun(ctx, domain.AgentRun{
		RunID: runID, Purpose: runPurposeAnswer, Status: domain.RunRunning,
	}); err != nil {
		return domain.Answer{}, appError(CodeInternal, "create agent run failed", err)
	}
	usage := domain.TokenUsage{}
	defer func() {
		if err := s.finishRun(ctx, runID, runPurposeAnswer, runStarted, usage, returnErr); err != nil && returnErr == nil {
			answer = domain.Answer{}
			returnErr = err
		}
		if returnErr != nil {
			returnErr = withRunID(returnErr, runID)
		}
	}()

	if configErr := s.ai.ConfigurationError(); configErr != nil {
		return domain.Answer{}, appError(CodeArkNotConfigured, configErr.Error(), configErr)
	}

	ordinal := 1
	searchStarted := time.Now()
	retrieved, err := s.retrieve(ctx, question, 5, KnowledgeScope{})
	s.recordEmbeddingUsage(ctx, runID, runPurposeQueryEmbedding, retrieved.Usage)
	if err != nil {
		if traceErr := s.addAgentEvent(ctx, runID, ordinal, "search_sources", domain.RunFailed, searchStarted, err, map[string]any{
			"mode": retrieved.Mode,
		}); traceErr != nil {
			return domain.Answer{}, traceErr
		}
		return domain.Answer{}, err
	}
	items := retrieved.Items
	if err := s.addAgentEvent(ctx, runID, ordinal, "search_sources", domain.RunSucceeded, searchStarted, nil, map[string]any{
		"result_count": len(items), "mode": retrieved.Mode, "keyword_hits": retrieved.KeywordHits, "semantic_hits": retrieved.SemanticHits,
	}); err != nil {
		return domain.Answer{}, err
	}
	ordinal++
	items, _ = currentKnowledge(items)
	if len(items) == 0 {
		return domain.Answer{}, appError(
			CodeNoGroundedKnowledge, "confirmed knowledge does not contain enough evidence for this question", nil,
		)
	}

	// The evidence quote on each item is the grounding: it is a validated
	// contiguous slice of the stored material, so it is the source passage.
	sources := items

	overrides, err := s.promptOverrides(ctx)
	if err != nil {
		return domain.Answer{}, err
	}
	llmStarted := time.Now()
	system := prompts.Answer(overrides)
	draft, answerUsage, err := s.ai.Answer(ctx, system.Text, question, sources)
	usage = answerUsage
	if err != nil {
		mapped := mapArkError(err, "Ark answer request failed", "Ark returned an invalid grounded answer")
		if traceErr := s.addAgentEvent(ctx, runID, ordinal, "ark_answer", domain.RunFailed, llmStarted, mapped, map[string]any{
			"model": s.ai.Model(),
		}); traceErr != nil {
			return domain.Answer{}, traceErr
		}
		return domain.Answer{}, mapped
	}
	if err := s.addAgentEvent(ctx, runID, ordinal, "ark_answer", domain.RunSucceeded, llmStarted, nil, map[string]any{
		"model": s.ai.Model(), "prompt_version": system.PromptVersion(),
	}); err != nil {
		return domain.Answer{}, err
	}
	ordinal++

	validationStarted := time.Now()
	allowed := make(map[int64]domain.KnowledgeItem, len(items))
	for _, item := range items {
		allowed[item.ID] = item
	}
	citations := make([]domain.Citation, 0, len(draft.CitationIDs))
	seen := make(map[int64]struct{})
	for _, id := range draft.CitationIDs {
		item, ok := allowed[id]
		if !ok {
			mapped := appError(CodeArkInvalidResponse, "Ark cited knowledge outside the retrieved context", nil)
			if traceErr := s.addAgentEvent(ctx, runID, ordinal, "citation_validation", domain.RunFailed, validationStarted, mapped, map[string]any{
				"citation_count": len(draft.CitationIDs),
			}); traceErr != nil {
				return domain.Answer{}, traceErr
			}
			return domain.Answer{}, mapped
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		citations = append(citations, domain.Citation{KnowledgeID: id, Title: item.Title, Evidence: item.Evidence})
	}
	if len(citations) == 0 {
		mapped := appError(CodeArkInvalidResponse, "Ark answer did not include a valid citation", nil)
		if traceErr := s.addAgentEvent(ctx, runID, ordinal, "citation_validation", domain.RunFailed, validationStarted, mapped, map[string]any{
			"citation_count": 0,
		}); traceErr != nil {
			return domain.Answer{}, traceErr
		}
		return domain.Answer{}, mapped
	}
	if err := s.addAgentEvent(ctx, runID, ordinal, "citation_validation", domain.RunSucceeded, validationStarted, nil, map[string]any{
		"citation_count": len(citations),
	}); err != nil {
		return domain.Answer{}, err
	}
	return domain.Answer{RunID: runID, Text: draft.Answer, Citations: citations}, nil
}
