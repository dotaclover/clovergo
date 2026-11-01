package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"clovergo/internal/domain"
	"clovergo/internal/repository"
	"clovergo/internal/workflows"
)

const (
	// The trace screen shows a short recent window, not a browsable archive:
	// five is enough to see what just happened without turning the page into
	// a log viewer.
	defaultRecentRuns = 5
	maxRecentRuns     = 50
)

func (s *Service) ListWorkflows() []workflows.Definition {
	return workflows.List()
}

// ListRecentRuns returns the newest runs, newest first, for the trace screen.
// The screen shows a fixed short window, so the limit is clamped here rather
// than trusted from the caller.
func (s *Service) ListRecentRuns(ctx context.Context, limit int) ([]domain.AgentRun, error) {
	if limit <= 0 {
		limit = defaultRecentRuns
	}
	if limit > maxRecentRuns {
		limit = maxRecentRuns
	}
	runs, err := s.repo.ListAgentRuns(ctx, limit)
	if err != nil {
		return nil, appError(CodeInternal, "list agent runs failed", err)
	}
	return runs, nil
}

func (s *Service) GetAgentRun(ctx context.Context, runID string) (*domain.AgentRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" || len(runID) > 128 {
		return nil, appError(CodeInvalidInput, "run id is required", nil)
	}
	run, err := s.repo.GetAgentRun(ctx, runID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, appError(CodeRunNotFound, "agent run was not found", err)
		}
		return nil, appError(CodeInternal, "read agent run failed", err)
	}
	return run, nil
}

func newRunID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "run_" + hex.EncodeToString(buffer), nil
}

func withRunID(err error, runID string) error {
	var appErr *AppError
	if errors.As(err, &appErr) {
		copy := *appErr
		copy.RunID = runID
		return &copy
	}
	return &AppError{Code: CodeInternal, Message: "agent run failed", Cause: err, RunID: runID}
}

func errorFields(err error) (string, string) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code, appErr.Message
	}
	return CodeInternal, "agent run failed"
}

func traceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
}

func (s *Service) addAgentEvent(
	ctx context.Context,
	runID string,
	ordinal int,
	name, status string,
	started time.Time,
	eventErr error,
	details any,
) error {
	detailsJSON := []byte("{}")
	if details != nil {
		encoded, err := json.Marshal(details)
		if err != nil {
			return appError(CodeInternal, "encode agent event failed", err)
		}
		detailsJSON = encoded
	}
	errorCode, errorMessage := "", ""
	if eventErr != nil {
		errorCode, errorMessage = errorFields(eventErr)
	}
	traceCtx, cancel := traceContext(ctx)
	defer cancel()
	err := s.repo.AddAgentEvent(traceCtx, domain.AgentEvent{
		RunID: runID, Ordinal: ordinal, Name: name, Status: status,
		LatencyMS: time.Since(started).Milliseconds(), ErrorCode: errorCode,
		ErrorMessage: errorMessage, Details: detailsJSON,
	})
	if err != nil {
		return appError(CodeInternal, "save agent event failed", err)
	}
	return nil
}

// finishRun records token usage and closes an Agent Run as succeeded or failed
// according to runErr. It returns an error only when the run itself could not
// be closed, so callers can fail an otherwise successful workflow rather than
// report a result whose trace is incomplete.
func (s *Service) finishRun(ctx context.Context, runID, purpose string, started time.Time, usage domain.TokenUsage, runErr error) error {
	return s.finishRunWithModel(ctx, runID, purpose, s.ai.Model(), started, usage, runErr)
}

// finishRunWithModel is finishRun for runs whose tokens belong to another
// model, such as embedding-only runs.
func (s *Service) finishRunWithModel(ctx context.Context, runID, purpose, model string, started time.Time, usage domain.TokenUsage, runErr error) error {
	status, errorCode, errorMessage := domain.RunSucceeded, "", ""
	if runErr != nil {
		status = domain.RunFailed
		errorCode, errorMessage = errorFields(runErr)
	}
	traceCtx, cancel := traceContext(ctx)
	defer cancel()
	if !usage.Empty() {
		if err := s.repo.RecordUsage(traceCtx, domain.UsageRecord{
			RunID: runID, Purpose: purpose, Model: model, TokenUsage: usage,
		}); err != nil {
			s.log.ErrorContext(traceCtx, "record token usage failed", "run_id", runID, "purpose", purpose, "error", err)
		}
	}
	if err := s.repo.CompleteAgentRun(traceCtx, runID, status, time.Since(started).Milliseconds(), errorCode, errorMessage, usage); err != nil {
		s.log.ErrorContext(traceCtx, "complete agent run failed", "run_id", runID, "purpose", purpose, "error", err)
		return &AppError{Code: CodeInternal, Message: "complete agent run failed", Cause: err, RunID: runID}
	}
	return nil
}
