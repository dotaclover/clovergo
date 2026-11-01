package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"clovergo/internal/domain"
	"clovergo/internal/prompts"
	"clovergo/internal/workflows"
)

const (
	runPurposeAgent = "agent_chat"
	// agentMaxSteps bounds model turns in one request. Each turn is a model
	// call, so this is also the cost ceiling of one question.
	agentMaxSteps        = 8
	agentMaxTurnTokens   = 4096
	agentMaxHistory      = 20
	agentMaxMessageRunes = 4000
	agentMaxToolRunes    = 6000
)

var citationPattern = regexp.MustCompile(`\[K(\d+)\]`)

type AgentChatInput struct {
	Messages []domain.AgentMessage `json:"messages"`
}

// agentTools is the fixed tool set. Every tool is read-only and reaches the
// library only through the same service paths the UI uses.
func agentTools() []domain.ToolSpec {
	return []domain.ToolSpec{
		{
			Name:        "search_knowledge",
			Description: "Search confirmed knowledge by keyword and meaning. Returns id, kind, title, body, tags and topics.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "search text"},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 8},
					"topic": map[string]any{"type": "string", "description": "optional: only knowledge on this exact topic (a topic name from library_overview)"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "get_knowledge",
			Description: "Read one knowledge item with its evidence quotes (each evidence has a document_id).",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"knowledge_id": map[string]any{"type": "integer"}},
				"required":   []string{"knowledge_id"},
			},
		},
		{
			Name:        "get_document",
			Description: "Read the stored text of one material, to see evidence in its original context.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"document_id": map[string]any{"type": "integer"}},
				"required":   []string{"document_id"},
			},
		},
		{
			Name:        "list_documents",
			Description: "List imported materials with their topic, status and how much knowledge each produced.",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "library_overview",
			Description: "Summarise what the knowledge base covers: every topic with how much knowledge sits on it, plus the knowledge types in use. Call this first for a broad question.",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

// AgentChat answers the latest user message with a bounded tool-calling loop.
// The model picks tools; the service runs them, traces each step, and checks
// that the final reply only cites knowledge the tools actually returned.
func (s *Service) AgentChat(ctx context.Context, input AgentChatInput) (reply *domain.AgentReply, returnErr error) {
	history, err := agentHistory(input.Messages)
	if err != nil {
		return nil, err
	}
	runID, runStarted, err := s.startWorkflowRun(ctx, workflows.AgentChatKey, runPurposeAgent, input)
	if err != nil {
		return nil, err
	}
	usage := domain.TokenUsage{}
	defer func() {
		if err := s.finishRun(ctx, runID, runPurposeAgent, runStarted, usage, returnErr); err != nil && returnErr == nil {
			reply = nil
			returnErr = err
		}
		if returnErr != nil {
			returnErr = withRunID(returnErr, runID)
		}
	}()
	if configErr := s.ai.ConfigurationError(); configErr != nil {
		return nil, appError(CodeArkNotConfigured, configErr.Error(), configErr)
	}

	overrides, err := s.promptOverrides(ctx)
	if err != nil {
		return nil, err
	}
	system := prompts.Agent(overrides)
	messages := append([]domain.ChatMessage{{Role: "system", Content: system.Text}}, history...)
	tools := agentTools()
	seen := make(map[int64]domain.KnowledgeItem)
	steps := make([]domain.AgentStep, 0)
	ordinal := 1
	citationRetried := false
	for turn := 1; turn <= agentMaxSteps; turn++ {
		started := time.Now()
		// Last turn: tool_choice "none" so the model must answer instead of
		// spending the final turn on calls whose results nobody reads.
		message, turnUsage, err := s.ai.Chat(ctx, messages, tools, agentMaxTurnTokens, turn == agentMaxSteps)
		usage.Add(turnUsage)
		if err != nil {
			mapped := mapArkError(err, "Ark agent request failed", "Ark returned an invalid agent turn")
			if traceErr := s.addAgentEvent(ctx, runID, ordinal, "model_turn", domain.RunFailed, started, mapped, map[string]any{
				"turn": turn, "model": s.ai.Model(),
			}); traceErr != nil {
				return nil, traceErr
			}
			return nil, mapped
		}
		if err := s.addAgentEvent(ctx, runID, ordinal, "model_turn", domain.RunSucceeded, started, nil, map[string]any{
			"turn": turn, "model": s.ai.Model(), "prompt_version": system.PromptVersion(), "tool_calls": len(message.ToolCalls),
		}); err != nil {
			return nil, err
		}
		ordinal++
		if len(message.ToolCalls) == 0 {
			citations, err := agentCitations(message.Content, seen)
			if err != nil {
				if traceErr := s.addAgentEvent(ctx, runID, ordinal, "citation_validation", domain.RunFailed, time.Now(), err, map[string]any{"retry": !citationRetried}); traceErr != nil {
					return nil, traceErr
				}
				if citationRetried || turn == agentMaxSteps {
					return nil, err
				}
				// One correction round: tell the model which ids it may cite.
				// The check itself stays in code and runs again on the new reply.
				citationRetried = true
				ordinal++
				messages = append(messages,
					domain.ChatMessage{Role: "assistant", Content: message.Content, ReasoningContent: message.ReasoningContent},
					domain.ChatMessage{Role: "user", Content: citationCorrection(seen)},
				)
				continue
			}
			if err := s.addAgentEvent(ctx, runID, ordinal, "citation_validation", domain.RunSucceeded, time.Now(), nil, map[string]any{
				"citation_count": len(citations),
			}); err != nil {
				return nil, err
			}
			text := strings.TrimSpace(message.Content)
			if text == "" {
				return nil, appError(CodeArkInvalidResponse, "Ark returned an empty agent reply", nil)
			}
			reply := &domain.AgentReply{RunID: runID, Reply: text, Citations: citations, Steps: steps}
			if err := s.recordAgentQA(ctx, reply, history[len(history)-1].Content, system.PromptVersion()); err != nil {
				// The answer already exists; losing its history row must not
				// throw the answer away.
				s.log.ErrorContext(ctx, "record agent history failed", "run_id", runID, "error", err)
			}
			return reply, nil
		}
		messages = append(messages, domain.ChatMessage{
			Role: "assistant", Content: message.Content, ReasoningContent: message.ReasoningContent, ToolCalls: message.ToolCalls,
		})
		for _, call := range message.ToolCalls {
			toolStarted := time.Now()
			result, summary, toolErr := s.runAgentTool(ctx, runID, call, seen)
			step := domain.AgentStep{Tool: call.Function.Name, Arguments: rawArguments(call.Function.Arguments), Summary: summary}
			details := map[string]any{"turn": turn, "arguments": step.Arguments, "summary": summary}
			status := domain.RunSucceeded
			if toolErr != nil {
				status = domain.RunFailed
				code, message := errorFields(toolErr)
				step.Error = message
				result = `{"error":` + strconv.Quote(message) + `}`
				if strings.HasPrefix(code, "ARK_") || code == CodeInternal {
					// Model or database failures are not something the agent
					// can work around; surface them instead of feeding them back.
					if err := s.addAgentEvent(ctx, runID, ordinal, "tool:"+call.Function.Name, status, toolStarted, toolErr, details); err != nil {
						return nil, err
					}
					return nil, toolErr
				}
			}
			if err := s.addAgentEvent(ctx, runID, ordinal, "tool:"+call.Function.Name, status, toolStarted, toolErr, details); err != nil {
				return nil, err
			}
			ordinal++
			steps = append(steps, step)
			messages = append(messages, domain.ChatMessage{Role: "tool", ToolCallID: call.ID, Content: result})
		}
	}
	return nil, appError(CodeAgentStepLimit, fmt.Sprintf("agent did not finish within %d steps", agentMaxSteps), nil)
}

// agentHistory validates the client-held conversation. It is untrusted: only
// user and assistant text is kept, and the last turn must be the user's.
func agentHistory(input []domain.AgentMessage) ([]domain.ChatMessage, error) {
	if len(input) == 0 || len(input) > agentMaxHistory {
		return nil, appError(CodeInvalidInput, fmt.Sprintf("messages must contain 1 to %d turns", agentMaxHistory), nil)
	}
	history := make([]domain.ChatMessage, 0, len(input))
	for _, message := range input {
		content := strings.TrimSpace(message.Content)
		if message.Role != "user" && message.Role != "assistant" {
			return nil, appError(CodeInvalidInput, "message role must be user or assistant", nil)
		}
		if content == "" || utf8.RuneCountInString(content) > agentMaxMessageRunes {
			return nil, appError(CodeInvalidInput, fmt.Sprintf("each message must be 1 to %d characters", agentMaxMessageRunes), nil)
		}
		history = append(history, domain.ChatMessage{Role: message.Role, Content: content})
	}
	if history[len(history)-1].Role != "user" {
		return nil, appError(CodeInvalidInput, "the last message must be from the user", nil)
	}
	return history, nil
}

func (s *Service) runAgentTool(ctx context.Context, runID string, call domain.ToolCall, seen map[int64]domain.KnowledgeItem) (string, string, error) {
	arguments := strings.TrimSpace(call.Function.Arguments)
	if arguments == "" {
		arguments = "{}"
	}
	invalid := func(err error) error {
		return appError(CodeToolInputInvalid, "tool arguments are invalid: "+err.Error(), err)
	}
	switch call.Function.Name {
	case "search_knowledge":
		var in struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
			Topic string `json:"topic"`
		}
		if err := json.Unmarshal([]byte(arguments), &in); err != nil {
			return "", "", invalid(err)
		}
		in.Query = strings.TrimSpace(in.Query)
		if in.Query == "" {
			return "", "", appError(CodeToolInputInvalid, "query is required", nil)
		}
		if in.Limit <= 0 || in.Limit > 8 {
			in.Limit = 5
		}
		retrieved, err := s.retrieve(ctx, in.Query, in.Limit, KnowledgeScope{Topic: strings.TrimSpace(in.Topic)})
		s.recordEmbeddingUsage(ctx, runID, runPurposeQueryEmbedding, retrieved.Usage)
		if err != nil {
			return "", "", err
		}
		items, _ := currentKnowledge(retrieved.Items)
		type hit struct {
			ID     int64    `json:"knowledge_id"`
			Kind   string   `json:"kind"`
			Title  string   `json:"title"`
			Body   string   `json:"body"`
			Tags   []string `json:"tags"`
			Topics []string `json:"topics"`
		}
		hits := make([]hit, 0, len(items))
		for _, item := range items {
			seen[item.ID] = item
			hits = append(hits, hit{item.ID, item.Kind, item.Title, item.Body, item.Tags, item.Topics})
		}
		return toolJSON(hits), fmt.Sprintf("%d results for %q", len(hits), in.Query), nil
	case "get_knowledge":
		var in struct {
			KnowledgeID int64 `json:"knowledge_id"`
		}
		if err := json.Unmarshal([]byte(arguments), &in); err != nil {
			return "", "", invalid(err)
		}
		item, err := s.GetKnowledge(ctx, in.KnowledgeID)
		if err != nil {
			return "", "", err
		}
		current, _ := currentKnowledge([]domain.KnowledgeItem{*item})
		if len(current) == 0 {
			return "", "", appError(CodeKnowledgeNotFound, "knowledge only cites superseded material", nil)
		}
		seen[item.ID] = current[0]
		type evidence struct {
			DocumentID int64  `json:"document_id"`
			Document   string `json:"document"`
			Quote      string `json:"quote"`
		}
		out := struct {
			ID       int64      `json:"knowledge_id"`
			Kind     string     `json:"kind"`
			Title    string     `json:"title"`
			Body     string     `json:"body"`
			Tags     []string   `json:"tags"`
			Evidence []evidence `json:"evidence"`
		}{ID: item.ID, Kind: item.Kind, Title: item.Title, Body: item.Body, Tags: item.Tags}
		for _, entry := range current[0].Evidence {
			out.Evidence = append(out.Evidence, evidence{entry.DocumentID, entry.DocumentName, entry.Quote})
		}
		return toolJSON(out), "K" + strconv.FormatInt(item.ID, 10) + " " + item.Title, nil
	case "get_document":
		var in struct {
			DocumentID int64 `json:"document_id"`
		}
		if err := json.Unmarshal([]byte(arguments), &in); err != nil {
			return "", "", invalid(err)
		}
		if in.DocumentID <= 0 {
			return "", "", appError(CodeToolInputInvalid, "document_id is required", nil)
		}
		source, err := s.GetSourceDocument(ctx, in.DocumentID)
		if err != nil {
			return "", "", err
		}
		text := source.Text
		if utf8.RuneCountInString(text) > agentMaxToolRunes {
			text = string([]rune(text)[:agentMaxToolRunes]) + "…"
		}
		return toolJSON(map[string]any{
			"document_id": source.DocumentID, "document": source.DocumentName, "topic": source.Topic, "text": text,
		}), fmt.Sprintf("document %d %s", source.DocumentID, source.DocumentName), nil
	case "list_documents":
		documents, err := s.ListDocuments(ctx)
		if err != nil {
			return "", "", err
		}
		type doc struct {
			ID        int64  `json:"id"`
			Filename  string `json:"filename"`
			Topic     string `json:"topic"`
			Status    string `json:"status"`
			Knowledge int    `json:"knowledge_count"`
		}
		out := make([]doc, 0, len(documents))
		for _, document := range documents {
			if len(out) == 100 {
				break
			}
			out = append(out, doc{document.ID, document.Filename, document.Topic, document.Status, document.UsefulCount})
		}
		return toolJSON(out), fmt.Sprintf("%d documents", len(out)), nil
	case "library_overview":
		topics, err := s.ListTopics(ctx)
		if err != nil {
			return "", "", err
		}
		type topic struct {
			Topic     string `json:"topic"`
			Knowledge int    `json:"knowledge_count"`
		}
		out := struct {
			Topics []topic  `json:"topics"`
			Kinds  []string `json:"kinds"`
		}{}
		kinds, err := s.Kinds(ctx)
		if err != nil {
			return "", "", err
		}
		for _, kind := range kinds {
			out.Kinds = append(out.Kinds, kind.ID)
		}
		for _, entry := range topics {
			out.Topics = append(out.Topics, topic{entry.Topic, entry.KnowledgeCount})
		}
		return toolJSON(out), fmt.Sprintf("%d topics", len(out.Topics)), nil
	}
	return "", "", appError(CodeToolInputInvalid, "unknown tool "+call.Function.Name, nil)
}

// agentCitations collects [K<id>] markers from the reply. A marker for an item
// no tool returned in this run means the model cited something it never saw.
func agentCitations(text string, seen map[int64]domain.KnowledgeItem) ([]domain.Citation, error) {
	citations := make([]domain.Citation, 0)
	added := make(map[int64]struct{})
	for _, match := range citationPattern.FindAllStringSubmatch(text, -1) {
		id, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return nil, appError(CodeArkInvalidResponse, "agent reply has a malformed citation", err)
		}
		item, ok := seen[id]
		if !ok {
			return nil, appError(CodeArkInvalidResponse, fmt.Sprintf("agent cited K%d, which no tool returned in this run", id), nil)
		}
		if _, dup := added[id]; dup {
			continue
		}
		added[id] = struct{}{}
		citations = append(citations, domain.Citation{KnowledgeID: id, Title: item.Title, Evidence: item.Evidence})
	}
	return citations, nil
}

func citationCorrection(seen map[int64]domain.KnowledgeItem) string {
	if len(seen) == 0 {
		return "Your reply cited knowledge that no tool returned in this run. No knowledge has been retrieved, so rewrite the reply without any [K<id>] citation, or call a tool first."
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = "K" + strconv.FormatInt(id, 10)
	}
	return "Your reply cited knowledge that no tool returned in this run. Rewrite the reply and cite only these ids: " + strings.Join(parts, ", ") + "."
}

func toolJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return `{"error":"encode tool result failed"}`
	}
	return string(data)
}

func rawArguments(arguments string) json.RawMessage {
	if strings.TrimSpace(arguments) == "" {
		return json.RawMessage("{}")
	}
	if json.Valid([]byte(arguments)) {
		return json.RawMessage(arguments)
	}
	encoded, _ := json.Marshal(arguments)
	return encoded
}

// recordAgentQA writes the answered question to the Q&A history, so the
// 知识问答 page can list what was asked and read the answer again. The history
// is a read-back log only; nothing in retrieval or drafting reads it.
func (s *Service) recordAgentQA(ctx context.Context, reply *domain.AgentReply, question, promptVersion string) error {
	if err := s.repo.SaveAgentQA(ctx, domain.AgentQA{
		RunID: reply.RunID, Question: question, Answer: reply.Reply,
		Citations: reply.Citations, Steps: reply.Steps,
		Model: s.ai.Model(), PromptVersion: promptVersion,
	}); err != nil {
		return appError(CodeInternal, "save qa history failed", err)
	}
	return nil
}

// ListQAHistory returns one page of past questions, newest first.
func (s *Service) ListQAHistory(ctx context.Context, limit, offset int) ([]domain.AgentQA, int, error) {
	limit = normalizeLimit(limit, 20)
	offset = normalizeOffset(offset)
	items, err := s.repo.ListAgentQA(ctx, limit, offset)
	if err != nil {
		return nil, 0, appError(CodeInternal, "list qa history failed", err)
	}
	total, err := s.repo.CountAgentQA(ctx)
	if err != nil {
		return nil, 0, appError(CodeInternal, "count qa history failed", err)
	}
	return items, total, nil
}
