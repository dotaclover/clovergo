package domain

import "encoding/json"

const (
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"
)

type TokenUsage struct {
	InputTokens  *int `json:"input_tokens,omitempty"`
	OutputTokens *int `json:"output_tokens,omitempty"`
	TotalTokens  *int `json:"total_tokens,omitempty"`
}

func (u *TokenUsage) Add(other TokenUsage) {
	u.InputTokens = addOptionalInt(u.InputTokens, other.InputTokens)
	u.OutputTokens = addOptionalInt(u.OutputTokens, other.OutputTokens)
	u.TotalTokens = addOptionalInt(u.TotalTokens, other.TotalTokens)
}

func addOptionalInt(left, right *int) *int {
	if left == nil && right == nil {
		return nil
	}
	value := 0
	if left != nil {
		value += *left
	}
	if right != nil {
		value += *right
	}
	return &value
}

type UsageRecord struct {
	RunID      string     `json:"run_id"`
	Purpose    string     `json:"purpose"`
	Model      string     `json:"model"`
	TokenUsage TokenUsage `json:"token_usage"`
	CreatedAt  string     `json:"created_at,omitempty"`
}

func (u TokenUsage) Empty() bool {
	return u.InputTokens == nil && u.OutputTokens == nil && u.TotalTokens == nil
}

type AgentRun struct {
	RunID        string       `json:"run_id"`
	Purpose      string       `json:"purpose"`
	Status       string       `json:"status"`
	LatencyMS    int64        `json:"latency_ms"`
	ErrorCode    string       `json:"error_code,omitempty"`
	ErrorMessage string       `json:"error_message,omitempty"`
	TokenUsage   *TokenUsage  `json:"token_usage,omitempty"`
	CreatedAt    string       `json:"created_at"`
	CompletedAt  *string      `json:"completed_at,omitempty"`
	Events       []AgentEvent `json:"events"`
}

type AgentEvent struct {
	ID           int64           `json:"id"`
	RunID        string          `json:"-"`
	Ordinal      int             `json:"ordinal"`
	Name         string          `json:"name"`
	Status       string          `json:"status"`
	LatencyMS    int64           `json:"latency_ms"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	Details      json.RawMessage `json:"details,omitempty"`
	CreatedAt    string          `json:"created_at"`
}

// WorkflowRun records which fixed workflow produced an artifact and with
// what input.
type WorkflowRun struct {
	RunID           string          `json:"run_id"`
	WorkflowKey     string          `json:"workflow_key"`
	WorkflowVersion string          `json:"workflow_version"`
	InputJSON       json.RawMessage `json:"input_json"`
	InputSHA256     string          `json:"input_sha256"`
	CreatedAt       string          `json:"created_at"`
}

// Artifact is the persisted output of a generation workflow.
type Artifact struct {
	ID            int64           `json:"id"`
	RunID         string          `json:"run_id"`
	Ordinal       int             `json:"ordinal"`
	Kind          string          `json:"kind"`
	SchemaVersion string          `json:"schema_version"`
	Title         string          `json:"title"`
	PayloadJSON   json.RawMessage `json:"-"`
	PayloadSHA256 string          `json:"payload_sha256"`
	Model         string          `json:"model"`
	PromptVersion string          `json:"prompt_version"`
	CreatedAt     string          `json:"created_at"`
}
