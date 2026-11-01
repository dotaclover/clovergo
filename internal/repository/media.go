package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"clovergo/internal/domain"
)

const ArtifactKindMediaDraft = "media_draft"

// insertArtifact stores an artifact inside tx.
func (s *SQLite) insertArtifact(ctx context.Context, tx *sql.Tx, artifact domain.Artifact) (int64, error) {
	if strings.TrimSpace(artifact.RunID) == "" {
		return 0, errors.New("artifact is incomplete")
	}
	if !json.Valid(artifact.PayloadJSON) {
		return 0, errors.New("artifact payload is not valid JSON")
	}
	hash := sha256.Sum256(artifact.PayloadJSON)
	result, err := tx.ExecContext(ctx, `INSERT INTO artifacts
		(run_id, ordinal, kind, schema_version, title, payload_json, payload_sha256, model, prompt_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, artifact.RunID, artifact.Ordinal, artifact.Kind,
		artifact.SchemaVersion, artifact.Title, string(artifact.PayloadJSON), hex.EncodeToString(hash[:]),
		artifact.Model, artifact.PromptVersion)
	if err != nil {
		return 0, fmt.Errorf("insert artifact: %w", err)
	}
	artifactID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return artifactID, nil
}

// CreatePendingMediaDraft stores a draft as soon as the request is accepted.
// The artifact is a stub with an empty payload; the background worker replaces
// it when the model returns. Status tells the list the article is being written.
func (s *SQLite) CreatePendingMediaDraft(ctx context.Context, draft domain.MediaDraft, artifact domain.Artifact) (*domain.MediaDraft, error) {
	if draft.RunID == "" || draft.RunID != artifact.RunID || draft.Platform == "" {
		return nil, errors.New("media draft is incomplete")
	}
	artifact.Kind = ArtifactKindMediaDraft
	artifact.PayloadJSON = []byte("{}")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	artifactID, err := s.insertArtifact(ctx, tx, artifact)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO media_drafts
		(run_id, artifact_id, platform, format, topic, angle, audience, payload_json, model, prompt_version, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, draft.RunID, artifactID,
		draft.Platform, draft.Format, draft.Topic, draft.Angle, draft.Audience, "{}", draft.Model, draft.PromptVersion,
		domain.MediaDraftRunning)
	if err != nil {
		return nil, fmt.Errorf("insert media draft: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetMediaDraft(ctx, id)
}

// CompleteMediaDraft writes the generated article over the pending row and its
// stub artifact. It only accepts a draft that is still running.
func (s *SQLite) CompleteMediaDraft(ctx context.Context, id int64, draft domain.MediaDraft, title string) (*domain.MediaDraft, error) {
	content, err := json.Marshal(domain.MediaDraftBody{Article: draft.Article, Gallery: draft.Gallery})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(content)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var runID string
	var artifactID int64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT run_id, artifact_id, status FROM media_drafts WHERE id = ?`, id).
		Scan(&runID, &artifactID, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != domain.MediaDraftRunning {
		return nil, ErrInvalidState
	}
	if _, err := tx.ExecContext(ctx, `UPDATE artifacts
		SET title = ?, payload_json = ?, payload_sha256 = ?, model = ?, prompt_version = ?
		WHERE id = ?`, title, string(content), hex.EncodeToString(hash[:]), draft.Model, draft.PromptVersion, artifactID); err != nil {
		return nil, fmt.Errorf("update artifact: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_drafts
		SET payload_json = ?, model = ?, prompt_version = ?, status = ?, error_message = ''
		WHERE id = ?`, string(content), draft.Model, draft.PromptVersion, domain.MediaDraftGenerated, id); err != nil {
		return nil, fmt.Errorf("complete media draft: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetMediaDraft(ctx, id)
}

// FailMediaDraft records why a background draft produced no article.
func (s *SQLite) FailMediaDraft(ctx context.Context, id int64, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media_drafts SET status = ?, error_message = ?
		WHERE id = ? AND status = ?`, domain.MediaDraftFailed, message, id, domain.MediaDraftRunning)
	return err
}

const mediaDraftSelect = `SELECT id, run_id, artifact_id, platform, format, topic, angle,
	audience, payload_json, model, prompt_version, status, error_message, created_at, wechat_media_id, wechat_pushed_at,
	wechat_publish_id, wechat_published_at FROM media_drafts`

// MarkDraftPublished records the publish_id returned by freepublish/submit.
func (s *SQLite) MarkDraftPublished(ctx context.Context, id int64, publishID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE media_drafts SET wechat_publish_id = ?, wechat_published_at = CURRENT_TIMESTAMP
		WHERE id = ?`, publishID, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkDraftPushed records the WeChat draft-box media_id of a draft.
func (s *SQLite) MarkDraftPushed(ctx context.Context, id int64, mediaID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE media_drafts SET wechat_media_id = ?, wechat_pushed_at = CURRENT_TIMESTAMP
		WHERE id = ?`, mediaID, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateMediaDraftBody replaces the content of a generated draft. An owner's
// edit goes through the same checks as model output before it reaches here.
func (s *SQLite) UpdateMediaDraftBody(ctx context.Context, id int64, body domain.MediaDraftBody, title string) (*domain.MediaDraft, error) {
	content, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(content)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var artifactID int64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT artifact_id, status FROM media_drafts WHERE id = ?`, id).
		Scan(&artifactID, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != domain.MediaDraftGenerated {
		return nil, ErrInvalidState
	}
	if _, err := tx.ExecContext(ctx, `UPDATE artifacts SET title = ?, payload_json = ?, payload_sha256 = ?
		WHERE id = ?`, title, string(content), hex.EncodeToString(hash[:]), artifactID); err != nil {
		return nil, fmt.Errorf("update artifact: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_drafts SET payload_json = ? WHERE id = ?`, string(content), id); err != nil {
		return nil, fmt.Errorf("update media draft: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetMediaDraft(ctx, id)
}

// DeleteMediaDraft removes a draft and its artifact. A running draft is
// refused: the worker would otherwise write into a row that no longer exists.
func (s *SQLite) DeleteMediaDraft(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var artifactID int64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT artifact_id, status FROM media_drafts WHERE id = ?`, id).
		Scan(&artifactID, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status == domain.MediaDraftRunning {
		return ErrInvalidState
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM media_drafts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete media draft: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, artifactID); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return tx.Commit()
}

func (s *SQLite) GetMediaDraft(ctx context.Context, id int64) (*domain.MediaDraft, error) {
	drafts, err := s.mediaDrafts(ctx, mediaDraftSelect+" WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(drafts) == 0 {
		return nil, ErrNotFound
	}
	return &drafts[0], nil
}

func (s *SQLite) ListMediaDrafts(ctx context.Context, platform string, limit, offset int) ([]domain.MediaDraft, error) {
	query := mediaDraftSelect + " WHERE 1=1"
	args := make([]any, 0, 3)
	if platform != "" {
		query += " AND platform = ?"
		args = append(args, platform)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}
	return s.mediaDrafts(ctx, query, args...)
}

// CountMediaDrafts returns how many drafts match, ignoring paging, so the list
// pager can show the total.
func (s *SQLite) CountMediaDrafts(ctx context.Context, platform string) (int, error) {
	query := "SELECT COUNT(*) FROM media_drafts WHERE 1=1"
	args := make([]any, 0, 1)
	if platform != "" {
		query += " AND platform = ?"
		args = append(args, platform)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *SQLite) mediaDrafts(ctx context.Context, query string, args ...any) ([]domain.MediaDraft, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	drafts := make([]domain.MediaDraft, 0)
	for rows.Next() {
		var draft domain.MediaDraft
		var payload string
		if err := rows.Scan(&draft.ID, &draft.RunID, &draft.ArtifactID, &draft.Platform,
			&draft.Format, &draft.Topic, &draft.Angle, &draft.Audience, &payload, &draft.Model, &draft.PromptVersion,
			&draft.Status, &draft.ErrorMessage, &draft.CreatedAt, &draft.WechatMediaID, &draft.WechatPushedAt,
			&draft.WechatPublishID, &draft.WechatPublishedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		var body domain.MediaDraftBody
		if err := json.Unmarshal([]byte(payload), &body); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("decode media draft %d: %w", draft.ID, err)
		}
		draft.Article = body.Article
		draft.Gallery = body.Gallery
		drafts = append(drafts, draft)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return drafts, nil
}
