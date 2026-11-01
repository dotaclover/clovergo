package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"clovergo/internal/platforms"
)

// PlatformSettings returns the owner settings and revision in effect for a
// platform, or found=false when the built-in default applies.
func (s *SQLite) PlatformSettings(ctx context.Context, platformID string) (platforms.Settings, int, bool, error) {
	var settingsJSON string
	var revision int
	err := s.db.QueryRowContext(ctx, `SELECT settings_json, revision FROM platform_style_revisions
		WHERE platform_id = ? ORDER BY revision DESC LIMIT 1`, platformID).Scan(&settingsJSON, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return platforms.Settings{}, 0, false, nil
	}
	if err != nil {
		return platforms.Settings{}, 0, false, err
	}
	var settings platforms.Settings
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		return platforms.Settings{}, 0, false, fmt.Errorf("decode platform %s settings: %w", platformID, err)
	}
	return settings, revision, true, nil
}

// SavePlatformSettings stores settings as the next revision when ifRevision is
// still current (0 means the built-in default is current).
func (s *SQLite) SavePlatformSettings(ctx context.Context, platformID string, ifRevision int, settings platforms.Settings, note string) (int, error) {
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision), 0) FROM platform_style_revisions
		WHERE platform_id = ?`, platformID).Scan(&current); err != nil {
		return 0, err
	}
	if current != ifRevision {
		return 0, ErrRevisionConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO platform_style_revisions (platform_id, revision, settings_json, note)
		VALUES (?, ?, ?, ?)`, platformID, current+1, string(settingsJSON), note); err != nil {
		return 0, err
	}
	return current + 1, tx.Commit()
}
