package tools

import (
	"context"
	"errors"
	"strings"

	"clovergo/internal/domain"
	"clovergo/internal/repository"
)

var ErrInvalidInput = errors.New("invalid local tool input")

type SearchSourcesInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type Local struct {
	repo repository.Repository
}

func NewLocal(repo repository.Repository) *Local {
	return &Local{repo: repo}
}

// SearchSources looks confirmed knowledge up by keyword. The library is a
// single space, so there is no pack or language to validate.
func (l *Local) SearchSources(ctx context.Context, input SearchSourcesInput) ([]domain.KnowledgeItem, error) {
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return nil, ErrInvalidInput
	}
	if input.Limit <= 0 {
		input.Limit = 5
	}
	if input.Limit > 20 {
		return nil, ErrInvalidInput
	}
	return l.repo.SearchKnowledge(ctx, input.Query, input.Limit)
}
