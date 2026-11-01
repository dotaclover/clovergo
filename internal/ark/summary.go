package ark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"clovergo/internal/domain"
)

// maxSummaryRunes caps the stored summary. The prompt asks for a short
// overview, so this only stops a malformed response from writing an essay into
// the material row.
const maxSummaryRunes = 1200

// Summarize writes the short overview a material is stored with. It runs once
// per upload, on the whole text, before the background extraction turns the
// same text into knowledge.
func (c *Client) Summarize(
	ctx context.Context,
	system string,
	text string,
) (string, domain.TokenUsage, error) {
	if strings.TrimSpace(system) == "" {
		return "", domain.TokenUsage{}, fmt.Errorf("%w: summary system prompt is empty", ErrInvalidResponse)
	}
	user := "SOURCE:\n" + text

	arguments, usage, err := c.callTool(ctx, system, user, summaryTool())
	if err != nil {
		return "", usage, err
	}
	var payload struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(arguments, &payload); err != nil {
		return "", usage, fmt.Errorf("%w: summary arguments do not match the schema: %v", ErrInvalidResponse, err)
	}
	summary := strings.TrimSpace(payload.Summary)
	if summary == "" {
		return "", usage, fmt.Errorf("%w: summary is empty", ErrInvalidResponse)
	}
	if runes := []rune(summary); len(runes) > maxSummaryRunes {
		summary = string(runes[:maxSummaryRunes])
	}
	return summary, usage, nil
}

func summaryTool() toolDefinition {
	return toolDefinition{
		Name:            "submit_summary",
		Description:     "Submit a short plain-text summary of one source material",
		DisableThinking: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary": map[string]any{"type": "string", "maxLength": maxSummaryRunes},
			},
			"required": []string{"summary"}, "additionalProperties": false,
		},
	}
}
