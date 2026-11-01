package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"clovergo/internal/domain"
)

// PromptOverrides returns the owner's layer replacements keyed by layer.
func (s *SQLite) PromptOverrides(ctx context.Context) (map[string]domain.PromptOverride, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT layer, text, revision, updated_at FROM prompt_overrides`)
	if err != nil {
		return nil, fmt.Errorf("list prompt overrides: %w", err)
	}
	defer rows.Close()
	result := map[string]domain.PromptOverride{}
	for rows.Next() {
		var item domain.PromptOverride
		if err := rows.Scan(&item.Layer, &item.Text, &item.Revision, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan prompt override: %w", err)
		}
		result[item.Layer] = item
	}
	return result, rows.Err()
}

// SavePromptOverride stores text as the next revision of one layer when
// ifRevision is still current (0 means the built-in text is current).
func (s *SQLite) SavePromptOverride(ctx context.Context, layer, text string, ifRevision int) (domain.PromptOverride, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PromptOverride{}, err
	}
	defer tx.Rollback()
	current := 0
	err = tx.QueryRowContext(ctx, `SELECT revision FROM prompt_overrides WHERE layer = ?`, layer).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.PromptOverride{}, fmt.Errorf("read prompt override: %w", err)
	}
	if current != ifRevision {
		return domain.PromptOverride{}, ErrRevisionConflict
	}
	next := current + 1
	if current == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO prompt_overrides (layer, text, revision) VALUES (?, ?, ?)`, layer, text, next)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE prompt_overrides SET text = ?, revision = ?, updated_at = CURRENT_TIMESTAMP
			WHERE layer = ?`, text, next, layer)
	}
	if err != nil {
		return domain.PromptOverride{}, fmt.Errorf("write prompt override: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.PromptOverride{}, err
	}
	return domain.PromptOverride{Layer: layer, Text: text, Revision: next}, nil
}

// ResetPromptOverride deletes one replacement, restoring the built-in text,
// when ifRevision is still current.
func (s *SQLite) ResetPromptOverride(ctx context.Context, layer string, ifRevision int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current := 0
	err = tx.QueryRowContext(ctx, `SELECT revision FROM prompt_overrides WHERE layer = ?`, layer).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read prompt override: %w", err)
	}
	if current != ifRevision {
		return ErrRevisionConflict
	}
	if current == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM prompt_overrides WHERE layer = ?`, layer); err != nil {
		return fmt.Errorf("delete prompt override: %w", err)
	}
	return tx.Commit()
}
