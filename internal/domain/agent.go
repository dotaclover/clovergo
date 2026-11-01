package domain

import "encoding/json"

// ChatMessage is one OpenAI-compatible chat message. The agent loop keeps the
// whole exchange in this shape: system, user, assistant (possibly with tool
// calls) and tool results.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ReasoningContent is the model's thinking pass. Some reasoning models
	// require it to be sent back within the same tool-calling turn.
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ToolSpec describes one tool the agent may call. Tools are defined in code;
// the model chooses among them but cannot add any.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// AgentMessage is one turn of the visible conversation, as the client sends
// it back on every request. Only user and assistant text is accepted.
type AgentMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AgentStep is one tool call the agent made while answering.
type AgentStep struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Summary   string          `json:"summary"`
	Error     string          `json:"error,omitempty"`
}

type AgentReply struct {
	RunID     string      `json:"run_id"`
	Reply     string      `json:"reply"`
	Citations []Citation  `json:"citations"`
	Steps     []AgentStep `json:"steps"`
}

// AgentQA is one answered question recorded for the 知识问答 history. It is a
// read-back log written after a run succeeds; nothing in retrieval, extraction
// or drafting reads it.
type AgentQA struct {
	ID            int64       `json:"id"`
	RunID         string      `json:"run_id"`
	Question      string      `json:"question"`
	Answer        string      `json:"answer"`
	Citations     []Citation  `json:"citations"`
	Steps         []AgentStep `json:"steps"`
	Model         string      `json:"model"`
	PromptVersion string      `json:"prompt_version"`
	CreatedAt     string      `json:"created_at"`
}
