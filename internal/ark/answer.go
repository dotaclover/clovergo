package ark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"clovergo/internal/domain"
)

func (c *Client) Answer(
	ctx context.Context,
	system string,
	question string,
	sources []domain.KnowledgeItem,
) (domain.AnswerDraft, domain.TokenUsage, error) {
	type contextItem struct {
		ID       int64             `json:"knowledge_id"`
		Title    string            `json:"title"`
		Body     string            `json:"body"`
		Evidence []domain.Evidence `json:"evidence"`
	}
	contextItems := make([]contextItem, 0, len(sources))
	for _, item := range sources {
		contextItems = append(contextItems, contextItem{
			ID: item.ID, Title: item.Title, Body: item.Body, Evidence: item.Evidence,
		})
	}
	contextJSON, err := json.Marshal(contextItems)
	if err != nil {
		return domain.AnswerDraft{}, domain.TokenUsage{}, err
	}
	if strings.TrimSpace(system) == "" {
		return domain.AnswerDraft{}, domain.TokenUsage{}, fmt.Errorf("%w: answer system prompt is empty", ErrInvalidResponse)
	}
	user := fmt.Sprintf("QUESTION: %s\nCONTEXT: %s", question, contextJSON)
	arguments, usage, err := c.callTool(ctx, system, user, answerTool())
	if err != nil {
		return domain.AnswerDraft{}, usage, err
	}
	var answer domain.AnswerDraft
	if err := json.Unmarshal(arguments, &answer); err != nil {
		return domain.AnswerDraft{}, usage, fmt.Errorf("%w: answer arguments do not match the schema: %v", ErrInvalidResponse, err)
	}
	answer.Answer = strings.TrimSpace(answer.Answer)
	if answer.Answer == "" || len(answer.CitationIDs) == 0 {
		return domain.AnswerDraft{}, usage, fmt.Errorf("%w: answer or citations are empty", ErrInvalidResponse)
	}
	return answer, usage, nil
}

func answerTool() toolDefinition {
	return toolDefinition{
		Name:        "submit_grounded_answer",
		Description: "Submit an answer grounded in confirmed knowledge",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"answer": map[string]any{"type": "string"},
				"citation_ids": map[string]any{
					"type": "array", "minItems": 1, "items": map[string]any{"type": "integer"},
				},
			},
			"required": []string{"answer", "citation_ids"}, "additionalProperties": false,
		},
	}
}
