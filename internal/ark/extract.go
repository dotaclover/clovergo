package ark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"clovergo/internal/domain"
)

// maxCandidatesPerDocument is a runaway guard, not a quota. The prompt
// deliberately states no number: a figure reads as a target and invites the
// model to split one knowledge unit into several to fill it. This bound only
// exists so a malformed response cannot balloon, and it is sized from the whole
// material the call reads: at the measured density of roughly one knowledge
// item per 280 characters, a 200k-character material yields about 700 items.
const maxCandidatesPerDocument = 1024

// ExtractCandidates runs one whole material through the extraction tool. It
// takes no knowledge space and no target language: the taxonomy comes from
// domain.Kinds and the output language is fixed in the prompt.
func (c *Client) ExtractCandidates(
	ctx context.Context,
	system string,
	text string,
) ([]domain.CandidateDraft, domain.TokenUsage, error) {
	if strings.TrimSpace(system) == "" {
		return nil, domain.TokenUsage{}, fmt.Errorf("%w: extraction system prompt is empty", ErrInvalidResponse)
	}
	user := "SOURCE:\n" + text

	arguments, usage, err := c.callTool(ctx, system, user, candidateTool())
	if err != nil {
		return nil, usage, err
	}
	var payload struct {
		Candidates []struct {
			Kind          string         `json:"kind"`
			CanonicalKey  string         `json:"canonical_key"`
			Title         string         `json:"title"`
			Body          string         `json:"body"`
			Tags          []string       `json:"tags"`
			EvidenceQuote string         `json:"evidence_quote"`
			Confidence    float64        `json:"confidence"`
			Metadata      map[string]any `json:"metadata"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(arguments, &payload); err != nil {
		return nil, usage, fmt.Errorf("%w: candidate arguments do not match the schema: %v", ErrInvalidResponse, err)
	}
	if len(payload.Candidates) > maxCandidatesPerDocument {
		return nil, usage, fmt.Errorf("%w: too many candidates", ErrInvalidResponse)
	}
	drafts := make([]domain.CandidateDraft, 0, len(payload.Candidates))
	for _, item := range payload.Candidates {
		metadata, err := json.Marshal(item.Metadata)
		if err != nil {
			return nil, usage, fmt.Errorf("%w: invalid candidate metadata", ErrInvalidResponse)
		}
		if item.Metadata == nil {
			metadata = []byte("{}")
		}
		drafts = append(drafts, domain.CandidateDraft{
			Kind: item.Kind, CanonicalKey: item.CanonicalKey, Title: item.Title, Body: item.Body,
			Tags: item.Tags, Metadata: metadata,
			EvidenceQuote: item.EvidenceQuote, Confidence: item.Confidence,
		})
	}
	return drafts, usage, nil
}

func candidateTool() toolDefinition {
	return toolDefinition{
		Name:            "submit_grounded_knowledge",
		Description:     "Submit evidence-grounded knowledge extracted from one source material",
		DisableThinking: true,
		// One call carries every item of a whole document; 32k output tokens
		// fits the item cap while a runaway response still ends as truncation.
		MaxTokens: 32768,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"candidates": map[string]any{
					"type": "array", "maxItems": maxCandidatesPerDocument,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"kind": map[string]any{
								"type": "string",
								"enum": domain.KindIDs(),
							},
							"canonical_key": map[string]any{"type": "string", "maxLength": 240},
							"title":         map[string]any{"type": "string", "maxLength": 240},
							"body":          map[string]any{"type": "string", "maxLength": 4000},
							"tags": map[string]any{
								"type": "array", "maxItems": domain.MaxTags,
								"items": map[string]any{"type": "string", "maxLength": 32},
							},
							"evidence_quote": map[string]any{"type": "string", "maxLength": 2000},
							"confidence":     map[string]any{"type": "number", "minimum": 0, "maximum": 1},
							"metadata":       map[string]any{"type": "object"},
						},
						"required":             []string{"kind", "canonical_key", "title", "body", "tags", "evidence_quote", "confidence", "metadata"},
						"additionalProperties": false,
					},
				},
			},
			"required": []string{"candidates"}, "additionalProperties": false,
		},
	}
}
