package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"clovergo/internal/domain"
)

func (s *SQLite) CreateWorkflowRun(ctx context.Context, workflow domain.WorkflowRun, run domain.AgentRun) error {
	if strings.TrimSpace(workflow.RunID) == "" || workflow.RunID != run.RunID ||
		strings.TrimSpace(workflow.WorkflowKey) == "" || strings.TrimSpace(workflow.WorkflowVersion) == "" ||
		!json.Valid(workflow.InputJSON) || strings.TrimSpace(workflow.InputSHA256) == "" {
		return errors.New("workflow run is incomplete")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_runs(run_id, purpose, status)
		VALUES (?, ?, ?)`, run.RunID, run.Purpose, run.Status); err != nil {
		return fmt.Errorf("insert agent run: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_runs
		(run_id, workflow_key, workflow_version, input_json, input_sha256)
		VALUES (?, ?, ?, ?, ?)`, workflow.RunID, workflow.WorkflowKey, workflow.WorkflowVersion,
		string(workflow.InputJSON), workflow.InputSHA256); err != nil {
		return fmt.Errorf("insert workflow run: %w", err)
	}
	return tx.Commit()
}

func (s *SQLite) CreateAgentRun(ctx context.Context, run domain.AgentRun) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_runs (run_id, purpose, status)
		VALUES (?, ?, ?)`, run.RunID, run.Purpose, run.Status)
	return err
}

func (s *SQLite) AddAgentEvent(ctx context.Context, event domain.AgentEvent) error {
	details := event.Details
	if len(details) == 0 {
		details = []byte("{}")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_run_events
		(run_id, ordinal, name, status, latency_ms, error_code, error_message, details_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, event.RunID, event.Ordinal, event.Name, event.Status,
		event.LatencyMS, event.ErrorCode, event.ErrorMessage, string(details))
	return err
}

func (s *SQLite) CompleteAgentRun(
	ctx context.Context,
	runID, status string,
	latencyMS int64,
	errorCode, errorMessage string,
	usage domain.TokenUsage,
) error {
	result, err := s.db.ExecContext(ctx, `UPDATE agent_runs SET status = ?, latency_ms = ?,
		error_code = ?, error_message = ?, input_tokens = ?, output_tokens = ?, total_tokens = ?,
		completed_at = CURRENT_TIMESTAMP WHERE run_id = ? AND status = 'running'`,
		status, latencyMS, errorCode, errorMessage, usage.InputTokens, usage.OutputTokens,
		usage.TotalTokens, runID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrInvalidState
	}
	return nil
}

// runColumns is the column list shared by the single-run read and the list
// read, so the two can never drift apart.
const runColumns = `run_id, purpose, status, latency_ms, error_code,
	error_message, input_tokens, output_tokens, total_tokens, created_at, completed_at`

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanAgentRun(row rowScanner) (domain.AgentRun, error) {
	var run domain.AgentRun
	var completedAt sql.NullString
	var inputTokens, outputTokens, totalTokens sql.NullInt64
	if err := row.Scan(&run.RunID, &run.Purpose, &run.Status, &run.LatencyMS, &run.ErrorCode, &run.ErrorMessage,
		&inputTokens, &outputTokens, &totalTokens, &run.CreatedAt, &completedAt); err != nil {
		return domain.AgentRun{}, err
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.String
	}
	if inputTokens.Valid || outputTokens.Valid || totalTokens.Valid {
		run.TokenUsage = &domain.TokenUsage{}
	}
	if inputTokens.Valid {
		value := int(inputTokens.Int64)
		run.TokenUsage.InputTokens = &value
	}
	if outputTokens.Valid {
		value := int(outputTokens.Int64)
		run.TokenUsage.OutputTokens = &value
	}
	if totalTokens.Valid {
		value := int(totalTokens.Int64)
		run.TokenUsage.TotalTokens = &value
	}
	return run, nil
}

// ListAgentRuns returns the most recent runs, newest first, without their step
// events: the list only needs the headline, and the detail route carries the
// events for whichever run the owner opens.
func (s *SQLite) ListAgentRuns(ctx context.Context, limit int) ([]domain.AgentRun, error) {
	// created_at only has second resolution, so rowid breaks ties and keeps
	// the order stable for runs recorded in the same second.
	rows, err := s.db.QueryContext(ctx, `SELECT `+runColumns+`
		FROM agent_runs ORDER BY created_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]domain.AgentRun, 0, limit)
	for rows.Next() {
		run, err := scanAgentRun(rows)
		if err != nil {
			return nil, err
		}
		run.Events = []domain.AgentEvent{}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *SQLite) GetAgentRun(ctx context.Context, runID string) (*domain.AgentRun, error) {
	run, err := scanAgentRun(s.db.QueryRowContext(ctx, `SELECT `+runColumns+`
		FROM agent_runs WHERE run_id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, ordinal, name, status, latency_ms,
		error_code, error_message, details_json, created_at
		FROM agent_run_events WHERE run_id = ? ORDER BY ordinal`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	run.Events = make([]domain.AgentEvent, 0)
	for rows.Next() {
		var event domain.AgentEvent
		var details string
		if err := rows.Scan(&event.ID, &event.RunID, &event.Ordinal, &event.Name, &event.Status,
			&event.LatencyMS, &event.ErrorCode, &event.ErrorMessage, &details, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.Details = []byte(details)
		run.Events = append(run.Events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *SQLite) RecordUsage(ctx context.Context, record domain.UsageRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO usage_ledger
		(run_id, purpose, model, input_tokens, output_tokens, total_tokens)
		VALUES (?, ?, ?, ?, ?, ?)`, record.RunID, record.Purpose, record.Model,
		record.TokenUsage.InputTokens, record.TokenUsage.OutputTokens, record.TokenUsage.TotalTokens)
	return err
}

// SaveAgentQA records one answered question for the Q&A history. Citations and
// steps are stored as JSON so the history row reads back exactly as it was
// answered.
func (s *SQLite) SaveAgentQA(ctx context.Context, record domain.AgentQA) error {
	citations, err := json.Marshal(record.Citations)
	if err != nil {
		return fmt.Errorf("encode citations: %w", err)
	}
	steps, err := json.Marshal(record.Steps)
	if err != nil {
		return fmt.Errorf("encode steps: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_qa_history
		(run_id, question, answer, citations_json, steps_json, model, prompt_version)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, record.RunID, record.Question, record.Answer,
		string(citations), string(steps), record.Model, record.PromptVersion)
	return err
}

// ListAgentQA returns one page of the Q&A history, newest first.
func (s *SQLite) ListAgentQA(ctx context.Context, limit, offset int) ([]domain.AgentQA, error) {
	query := `SELECT id, run_id, question, answer, citations_json, steps_json, model, prompt_version, created_at
		FROM agent_qa_history ORDER BY created_at DESC, id DESC LIMIT ?`
	args := []any{limit}
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.AgentQA, 0, limit)
	for rows.Next() {
		var record domain.AgentQA
		var citations, steps string
		if err := rows.Scan(&record.ID, &record.RunID, &record.Question, &record.Answer,
			&citations, &steps, &record.Model, &record.PromptVersion, &record.CreatedAt); err != nil {
			return nil, err
		}
		if citations != "" {
			if err := json.Unmarshal([]byte(citations), &record.Citations); err != nil {
				return nil, err
			}
		}
		if steps != "" {
			if err := json.Unmarshal([]byte(steps), &record.Steps); err != nil {
				return nil, err
			}
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

// CountAgentQA returns the full history size so the pager can show the total.
func (s *SQLite) CountAgentQA(ctx context.Context) (int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_qa_history`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}
