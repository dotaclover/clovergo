package repository

import (
	"context"
)

// LibraryStats is the whole-library size, used by the workspace header and the
// vector-coverage page now that knowledge spaces are gone.
type LibraryStats struct {
	Documents int `json:"documents"`
	// Knowledge counts confirmed knowledge items.
	Knowledge int `json:"knowledge"`
	// Vectors counts knowledge items embedded with the configured model at the
	// configured dimensions; stale vectors from another model are excluded.
	Vectors int `json:"vectors"`
	// DocumentVectors counts materials embedded over their summary with the
	// configured model. Materials have their own vector so a query can match a
	// material whose knowledge does not repeat its words.
	DocumentVectors int `json:"document_vectors"`
	// DocumentVectorsTotal counts materials that can be embedded at all: only a
	// material with a summary gets a vector, so this is the honest denominator
	// for coverage. DocumentsWithoutSummary explains the gap to Documents.
	DocumentVectorsTotal int `json:"document_vectors_total"`
	// Pending counts materials archived but not extracted yet. Uploading only
	// archives, so this is a resting state, not a background job.
	Pending int `json:"pending"`
	// Processing counts materials whose extraction is running right now.
	Processing int `json:"processing"`
}

func (s *SQLite) LibraryStats(ctx context.Context, model string, dimensions int) (LibraryStats, error) {
	var stats LibraryStats
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM documents").Scan(&stats.Documents); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM knowledge_items k WHERE "+currentKnowledgeClause).Scan(&stats.Knowledge); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_vectors v
		JOIN knowledge_items k ON k.id = v.knowledge_item_id
		WHERE v.model = ? AND v.dimensions = ? AND `+currentKnowledgeClause, model, dimensions).Scan(&stats.Vectors); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM document_vectors
		WHERE model = ? AND dimensions = ?`, model, dimensions).Scan(&stats.DocumentVectors); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM documents WHERE status = 'ready'").Scan(&stats.Pending); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM documents WHERE status = 'processing'").Scan(&stats.Processing); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM documents WHERE summary <> ''").Scan(&stats.DocumentVectorsTotal); err != nil {
		return stats, err
	}
	return stats, nil
}
