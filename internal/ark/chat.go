package ark

import (
	"context"

	"clovergo/internal/domain"
)

// Chat runs one turn of a tool-calling conversation: the model may answer in
// text or ask for any of the given tools. Unlike callTool nothing is forced;
// the caller owns the loop and validates whatever comes back.
func (c *Client) Chat(ctx context.Context, messages []domain.ChatMessage, tools []domain.ToolSpec, maxTokens int, answerOnly bool) (domain.ChatMessage, domain.TokenUsage, error) {
	if err := c.ConfigurationError(); err != nil {
		return domain.ChatMessage{}, domain.TokenUsage{}, err
	}
	specs := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		specs = append(specs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters,
			},
		})
	}
	payload := map[string]any{
		"model":       c.model,
		"messages":    messages,
		"temperature": 0.2,
	}
	if len(specs) > 0 {
		payload["tools"] = specs
		payload["tool_choice"] = "auto"
		if answerOnly {
			payload["tool_choice"] = "none"
		}
	}
	if maxTokens > 0 {
		payload["max_tokens"] = maxTokens
	}
	return c.send(ctx, payload)
}
