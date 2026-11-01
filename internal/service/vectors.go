package service

import (
	"context"
	"sync"
	"time"

	"clovergo/internal/domain"
	"clovergo/internal/repository"
)

const (
	runPurposeVectorReindex = "vector_reindex"
	embeddingConcurrency    = 4
	maxReindexBatch         = 200
)

type embedOutcome struct {
	Embedded   int
	Failed     int
	FirstError error
	Usage      domain.TokenUsage
}

// embedTasks embeds knowledge items with bounded concurrency and stores each
// vector as soon as it arrives.
func (s *Service) embedTasks(ctx context.Context, tasks []repository.VectorTask) embedOutcome {
	var outcome embedOutcome
	var mu sync.Mutex
	work := make(chan repository.VectorTask)
	var wg sync.WaitGroup
	for worker := 0; worker < embeddingConcurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range work {
				vector, usage, err := s.embedder.Embed(ctx, repository.VectorText(task.Title, task.Body))
				if err != nil {
					err = mapArkError(err, "Ark embedding request failed", "Ark returned an invalid embedding")
				} else if err = s.repo.SaveKnowledgeVector(ctx, task, s.embedder.Model(), vector); err != nil {
					err = appError(CodeInternal, "save knowledge vector failed", err)
				}
				mu.Lock()
				outcome.Usage.Add(usage)
				if err != nil {
					outcome.Failed++
					if outcome.FirstError == nil {
						outcome.FirstError = err
					}
				} else {
					outcome.Embedded++
				}
				mu.Unlock()
			}
		}()
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			break
		}
		work <- task
	}
	close(work)
	wg.Wait()
	return outcome
}

// embedDocumentTasks embeds materials over their summaries with bounded
// concurrency and stores each vector as soon as it arrives. It mirrors
// embedTasks exactly; documents and knowledge each keep their own vector row.
func (s *Service) embedDocumentTasks(ctx context.Context, tasks []repository.DocumentVectorTask) embedOutcome {
	var outcome embedOutcome
	var mu sync.Mutex
	work := make(chan repository.DocumentVectorTask)
	var wg sync.WaitGroup
	for worker := 0; worker < embeddingConcurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range work {
				vector, usage, err := s.embedder.Embed(ctx, task.Summary)
				if err != nil {
					err = mapArkError(err, "Ark embedding request failed", "Ark returned an invalid embedding")
				} else if err = s.repo.SaveDocumentVector(ctx, task, s.embedder.Model(), vector); err != nil {
					err = appError(CodeInternal, "save material vector failed", err)
				}
				mu.Lock()
				outcome.Usage.Add(usage)
				if err != nil {
					outcome.Failed++
					if outcome.FirstError == nil {
						outcome.FirstError = err
					}
				} else {
					outcome.Embedded++
				}
				mu.Unlock()
			}
		}()
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			break
		}
		work <- task
	}
	close(work)
	wg.Wait()
	return outcome
}

// Vectorization is manual and off the import path. Nothing embeds on its own
// when a material is imported or knowledge is published, so those steps cost
// exactly one summary or extraction call. Embeddings are created only when the
// operator runs /vectors/reindex (ReindexAllVectors); retrieval degrades to
// keyword until then.

type ReindexResult struct {
	RunID      string `json:"run_id"`
	Model      string `json:"model"`
	Embedded   int    `json:"embedded"`
	Failed     int    `json:"failed"`
	Remaining  int    `json:"remaining"`
	FirstError string `json:"first_error,omitempty"`
}

// ReindexVectors embeds up to limit items whose vector is missing or stale,
// for example after enabling semantic search or changing the model. Callers
// repeat it until Remaining is zero.
func (s *Service) ReindexVectors(ctx context.Context, limit int) (result ReindexResult, returnErr error) {
	if !s.embedder.Configured() {
		return result, appError(CodeEmbeddingNotConfigured, "set ARK_EMBEDDING_MODEL to enable semantic search", nil)
	}
	if limit <= 0 || limit > maxReindexBatch {
		limit = maxReindexBatch
	}
	result.Model = s.embedder.Model()
	tasks, pending, err := s.repo.VectorTasks(ctx, s.embedder.Model(), s.embedder.Dimensions(), limit, nil)
	if err != nil {
		return result, appError(CodeInternal, "find knowledge to embed failed", err)
	}
	if len(tasks) == 0 {
		return result, nil
	}
	runID, err := newRunID()
	if err != nil {
		return result, appError(CodeInternal, "generate run id failed", err)
	}
	result.RunID = runID
	started := time.Now()
	if err := s.repo.CreateAgentRun(ctx, domain.AgentRun{RunID: runID, Purpose: runPurposeVectorReindex, Status: domain.RunRunning}); err != nil {
		return result, appError(CodeInternal, "create run failed", err)
	}
	var usage domain.TokenUsage
	defer func() {
		if err := s.finishRunWithModel(ctx, runID, runPurposeVectorReindex, s.embedder.Model(), started, usage, returnErr); err != nil && returnErr == nil {
			returnErr = err
		}
		if returnErr != nil {
			returnErr = withRunID(returnErr, runID)
		}
	}()
	outcome := s.embedTasks(ctx, tasks)
	usage = outcome.Usage
	result.Embedded, result.Failed = outcome.Embedded, outcome.Failed
	result.Remaining = pending - outcome.Embedded
	if outcome.FirstError != nil {
		mapped := outcome.FirstError
		result.FirstError = mapped.Error()
		if outcome.Embedded == 0 {
			_ = s.addAgentEvent(ctx, runID, 1, "embed_knowledge", domain.RunFailed, started, mapped, map[string]any{"failed": outcome.Failed})
			return result, mapped
		}
	}
	if err := s.addAgentEvent(ctx, runID, 1, "embed_knowledge", domain.RunSucceeded, started, nil, map[string]any{
		"model": s.embedder.Model(), "embedded": outcome.Embedded, "failed": outcome.Failed, "remaining": result.Remaining,
	}); err != nil {
		return result, err
	}
	return result, nil
}

// ReindexDocumentVectors embeds up to limit materials whose summary vector is
// missing or stale. It is the material-side twin of ReindexVectors, sharing
// the same run purpose and event name shape.
func (s *Service) ReindexDocumentVectors(ctx context.Context, limit int) (result ReindexResult, returnErr error) {
	if !s.embedder.Configured() {
		return result, appError(CodeEmbeddingNotConfigured, "set ARK_EMBEDDING_MODEL to enable semantic search", nil)
	}
	if limit <= 0 || limit > maxReindexBatch {
		limit = maxReindexBatch
	}
	result.Model = s.embedder.Model()
	tasks, pending, err := s.repo.DocumentVectorTasks(ctx, s.embedder.Model(), s.embedder.Dimensions(), limit)
	if err != nil {
		return result, appError(CodeInternal, "find materials to embed failed", err)
	}
	if len(tasks) == 0 {
		return result, nil
	}
	runID, err := newRunID()
	if err != nil {
		return result, appError(CodeInternal, "generate run id failed", err)
	}
	result.RunID = runID
	started := time.Now()
	if err := s.repo.CreateAgentRun(ctx, domain.AgentRun{RunID: runID, Purpose: runPurposeVectorReindex, Status: domain.RunRunning}); err != nil {
		return result, appError(CodeInternal, "create run failed", err)
	}
	var usage domain.TokenUsage
	defer func() {
		if err := s.finishRunWithModel(ctx, runID, runPurposeVectorReindex, s.embedder.Model(), started, usage, returnErr); err != nil && returnErr == nil {
			returnErr = err
		}
		if returnErr != nil {
			returnErr = withRunID(returnErr, runID)
		}
	}()
	outcome := s.embedDocumentTasks(ctx, tasks)
	usage = outcome.Usage
	result.Embedded, result.Failed = outcome.Embedded, outcome.Failed
	result.Remaining = pending - outcome.Embedded
	if outcome.FirstError != nil {
		mapped := outcome.FirstError
		result.FirstError = mapped.Error()
		if outcome.Embedded == 0 {
			_ = s.addAgentEvent(ctx, runID, 1, "embed_document", domain.RunFailed, started, mapped, map[string]any{"failed": outcome.Failed})
			return result, mapped
		}
	}
	if err := s.addAgentEvent(ctx, runID, 1, "embed_document", domain.RunSucceeded, started, nil, map[string]any{
		"model": s.embedder.Model(), "embedded": outcome.Embedded, "failed": outcome.Failed, "remaining": result.Remaining,
	}); err != nil {
		return result, err
	}
	return result, nil
}

// ReindexAllVectors embeds pending knowledge and pending materials in one call,
// so the settings page's manual reindex catches both up at once. Its totals sum
// the two sides; the run id is whichever side created a run. A failure on the
// knowledge side does not stop the material side; the first error is returned
// after both have run.
func (s *Service) ReindexAllVectors(ctx context.Context, limit int) (ReindexResult, error) {
	knowledge, knowledgeErr := s.ReindexVectors(ctx, limit)
	documents, documentErr := s.ReindexDocumentVectors(ctx, limit)
	combined := knowledge
	combined.Embedded += documents.Embedded
	combined.Failed += documents.Failed
	combined.Remaining += documents.Remaining
	if combined.FirstError == "" {
		combined.FirstError = documents.FirstError
	}
	if combined.RunID == "" {
		combined.RunID = documents.RunID
	}
	if knowledgeErr != nil {
		return combined, knowledgeErr
	}
	return combined, documentErr
}

func (s *Service) recordEmbeddingUsage(ctx context.Context, runID, purpose string, usage domain.TokenUsage) {
	if usage.Empty() {
		return
	}
	traceCtx, cancel := traceContext(ctx)
	defer cancel()
	if err := s.repo.RecordUsage(traceCtx, domain.UsageRecord{
		RunID: runID, Purpose: purpose, Model: s.embedder.Model(), TokenUsage: usage,
	}); err != nil {
		s.log.ErrorContext(traceCtx, "record embedding usage failed", "run_id", runID, "error", err)
	}
}
