package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"clovergo/internal/domain"
)

// FindLatestDocumentBySource returns the newest version stored under a source
// key; a material is "unchanged" when that version has the same sha256.
func (s *SQLite) FindLatestDocumentBySource(ctx context.Context, sourceKey string) (*domain.Document, error) {
	row := s.db.QueryRowContext(ctx, documentSelect+" WHERE d.source_key = ? ORDER BY d.version DESC LIMIT 1", sourceKey)
	return scanDocument(row)
}

// CreateDocument stores one material version in 'ready'. The material is stored
// whole -- there is no chunk table any more -- and the summary was written
// before this call.
func (s *SQLite) CreateDocument(ctx context.Context, document domain.Document) (*domain.Document, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1
		FROM documents WHERE source_key = ?`, document.SourceKey).Scan(&document.Version); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO documents
		(filename, source_key, version, sha256, media_type, language, topic, summary, stored_path, status,
			root_id, rel_path, origin_size, origin_mtime)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'ready', ?, ?, ?, ?)`, document.Filename, document.SourceKey, document.Version,
		document.SHA256, document.MediaType, document.Language, document.Topic, document.Summary, document.StoredPath,
		document.RootID, document.RelPath, document.OriginSize, document.OriginMTime)
	if err != nil {
		return nil, err
	}
	documentID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetDocument(ctx, documentID)
}

func (s *SQLite) GetDocument(ctx context.Context, id int64) (*domain.Document, error) {
	row := s.db.QueryRowContext(ctx, documentSelect+" WHERE d.id = ?", id)
	return scanDocument(row)
}

func (s *SQLite) ListDocuments(ctx context.Context) ([]domain.Document, error) {
	rows, err := s.db.QueryContext(ctx, documentSelect+" ORDER BY d.id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	documents := make([]domain.Document, 0)
	for rows.Next() {
		document, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		documents = append(documents, *document)
	}
	return documents, rows.Err()
}

const documentSelect = `SELECT d.id, d.filename, d.source_key, d.version, d.sha256, d.media_type, d.language, d.topic,
	d.summary, d.stored_path, d.status, d.error_message, d.attempts, d.created_at, d.processed_at, d.useful_count,
	d.use_count, d.processing_run_id, d.root_id, d.rel_path, d.missing, d.origin_size, d.origin_mtime
	FROM documents d`

type scanner interface {
	Scan(...any) error
}

func scanDocument(row scanner) (*domain.Document, error) {
	var document domain.Document
	err := row.Scan(&document.ID, &document.Filename, &document.SourceKey, &document.Version,
		&document.SHA256, &document.MediaType, &document.Language, &document.Topic, &document.Summary,
		&document.StoredPath, &document.Status, &document.ErrorMessage, &document.Attempts, &document.CreatedAt,
		&document.ProcessedAt, &document.UsefulCount, &document.UseCount, &document.ProcessingRunID,
		&document.RootID, &document.RelPath, &document.Missing, &document.OriginSize, &document.OriginMTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &document, err
}

// ClaimDocumentProcessing takes ownership of a document for one run. attempts
// counts how many times a worker has claimed it, which bounds automatic
// retries.
func (s *SQLite) ClaimDocumentProcessing(ctx context.Context, id int64, runID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE documents
		SET status = 'processing', error_message = '', useful_count = 0,
			processing_run_id = ?, attempts = attempts + 1
		WHERE id = ? AND status NOT IN ('processing', 'published', 'processed_empty')`, runID, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		return nil
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM documents WHERE id = ?", id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	return ErrInvalidState
}

func (s *SQLite) UpdateDocumentProcessing(
	ctx context.Context,
	id int64,
	status, message string,
	usefulCount int,
	runID string,
) error {
	result, err := s.db.ExecContext(ctx, `UPDATE documents
		SET status = ?, error_message = ?, useful_count = ?, processing_run_id = ?,
			processed_at = CASE WHEN ? IN ('published', 'processed_empty') THEN CURRENT_TIMESTAMP ELSE processed_at END
		WHERE id = ?`, status, message, usefulCount, runID, status, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrNotFound
	}
	return nil
}

// RequeueDocument resets a material to ready unless it is being processed. The
// state test is part of the UPDATE, so two concurrent requests cannot both pass
// it. It reports false when the material is currently processing.
func (s *SQLite) RequeueDocument(ctx context.Context, id int64, processingStatus, readyStatus string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE documents
		SET status = ?, error_message = '', useful_count = 0, processing_run_id = ''
		WHERE id = ? AND status != ?`, readyStatus, id, processingStatus)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed == 1 {
		return true, nil
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM documents WHERE id = ?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	return false, nil
}

// SaveDocumentKnowledge stores one material's validated knowledge in a single
// transaction. Each draft carries its evidence quote inline, so there is no
// evidence table: the quote and its rune offsets live on the knowledge row.
// A row whose fingerprint already exists is skipped (INSERT OR IGNORE).
// Re-extracting a material replaces its knowledge: the material's previous
// rows are deleted in the same transaction (vectors cascade, the FTS triggers
// follow), so stale items from an earlier extraction never linger. Knowledge
// of older versions of the same source belongs to other document IDs and is
// untouched; it stays superseded as before.
func (s *SQLite) SaveDocumentKnowledge(ctx context.Context, documentID int64, drafts []domain.KnowledgeDraft) (published, ignored int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM knowledge_items WHERE document_id = ?", documentID); err != nil {
		return 0, 0, err
	}
	for _, draft := range drafts {
		metadata := string(draft.Metadata)
		if metadata == "" {
			metadata = "{}"
		}
		result, execErr := tx.ExecContext(ctx, `INSERT OR IGNORE INTO knowledge_items
			(document_id, kind, title, body, tags_json, metadata_json, fingerprint, evidence_quote, quote_start, quote_end)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, documentID, draft.Kind, draft.Title, draft.Body,
			encodeTags(draft.Tags), metadata, draft.Fingerprint, draft.EvidenceQuote, draft.QuoteStart, draft.QuoteEnd)
		if execErr != nil {
			return 0, 0, execErr
		}
		changed, affectedErr := result.RowsAffected()
		if affectedErr != nil {
			return 0, 0, affectedErr
		}
		if changed == 1 {
			published++
		} else {
			ignored++
		}
	}
	return published, ignored, tx.Commit()
}

// CountKnowledgeByDocument is how much knowledge one material contributed,
// used as its useful_count.
func (s *SQLite) CountKnowledgeByDocument(ctx context.Context, documentID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM knowledge_items WHERE document_id = ?", documentID).Scan(&count)
	return count, err
}

// IncrementDocumentUse records that a draft was written from each of these
// materials' original text. One statement keeps the counters in step with the
// saved draft; unknown ids are ignored.
func (s *SQLite) IncrementDocumentUse(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE documents SET use_count = use_count + 1 WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...); err != nil {
		return fmt.Errorf("increment document use: %w", err)
	}
	return nil
}

// UpdateDocumentTopic re-files one material onto a different shelf. Topics are
// free text chosen by the operator, so there is nothing to validate beyond
// normalizing whitespace; an empty topic means "unfiled".
func (s *SQLite) UpdateDocumentTopic(ctx context.Context, id int64, topic string) (*domain.Document, error) {
	result, err := s.db.ExecContext(ctx, "UPDATE documents SET topic = ? WHERE id = ?", topic, id)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, ErrNotFound
	}
	return s.GetDocument(ctx, id)
}

// RenameDocument changes only the display name; source_key and the stored file
// are untouched, so versions and evidence offsets are unaffected.
func (s *SQLite) RenameDocument(ctx context.Context, id int64, filename string) (*domain.Document, error) {
	result, err := s.db.ExecContext(ctx, "UPDATE documents SET filename = ? WHERE id = ?", filename, id)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, ErrNotFound
	}
	return s.GetDocument(ctx, id)
}

// DeleteDocument removes one material; its knowledge and vectors go with it
// through ON DELETE CASCADE. A material that is being processed is refused
// (ErrInvalidState). It returns the stored file path and whether another
// material still points at the same file, so the caller can remove the file.
func (s *SQLite) DeleteDocument(ctx context.Context, id int64, processingStatus string) (storedPath string, shared bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, "SELECT stored_path, status FROM documents WHERE id = ?", id).Scan(&storedPath, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, ErrNotFound
		}
		return "", false, err
	}
	if status == processingStatus {
		return "", false, ErrInvalidState
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM documents WHERE id = ?", id); err != nil {
		return "", false, err
	}
	var others int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM documents WHERE stored_path = ?", storedPath).Scan(&others); err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return storedPath, others > 0, nil
}

// ListTopics returns every non-empty topic with how many knowledge items were
// extracted from material on it, busiest first.
func (s *SQLite) ListTopics(ctx context.Context) ([]TopicSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.topic, COUNT(k.id)
		FROM documents d
		JOIN knowledge_items k ON k.document_id = d.id
		WHERE d.topic <> ''
		GROUP BY d.topic
		ORDER BY COUNT(k.id) DESC, d.topic`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	topics := make([]TopicSummary, 0)
	for rows.Next() {
		var summary TopicSummary
		if err := rows.Scan(&summary.Topic, &summary.KnowledgeCount); err != nil {
			return nil, err
		}
		topics = append(topics, summary)
	}
	return topics, rows.Err()
}
