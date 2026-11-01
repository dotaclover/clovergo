package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"clovergo/internal/domain"
)

// WechatSettings reads the single settings row. A row that has never been
// written returns zero values, which means "not configured".
func (s *SQLite) WechatSettings(ctx context.Context) (*domain.WechatSettings, error) {
	var settings domain.WechatSettings
	err := s.db.QueryRowContext(ctx, `SELECT app_id, app_secret, author, thumb_media_id, proxy_url, updated_at
		FROM wechat_settings WHERE id = 1`).
		Scan(&settings.AppID, &settings.AppSecret, &settings.Author, &settings.ThumbMediaID, &settings.ProxyURL, &settings.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return &domain.WechatSettings{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read wechat settings: %w", err)
	}
	return &settings, nil
}

// SaveWechatSettings writes the single settings row, replacing whatever was
// there.
func (s *SQLite) SaveWechatSettings(ctx context.Context, settings domain.WechatSettings) (*domain.WechatSettings, error) {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO wechat_settings
		(id, app_id, app_secret, author, thumb_media_id, proxy_url, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			app_id = excluded.app_id,
			app_secret = excluded.app_secret,
			author = excluded.author,
			thumb_media_id = excluded.thumb_media_id,
			proxy_url = excluded.proxy_url,
			updated_at = excluded.updated_at`,
		settings.AppID, settings.AppSecret, settings.Author, settings.ThumbMediaID, settings.ProxyURL); err != nil {
		return nil, fmt.Errorf("save wechat settings: %w", err)
	}
	return s.WechatSettings(ctx)
}
