package repository

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
)

// VectorText is the exact text embedded for a knowledge item. Its hash marks a
// stored vector stale when the item's text changes.
func VectorText(title, body string) string {
	return title + "\n" + body
}

func vectorTextHash(title, body string) string {
	sum := sha256.Sum256([]byte(VectorText(title, body)))
	return hex.EncodeToString(sum[:])
}

// documentVectorTextHash keys a material's vector to its summary: the summary
// is the only text embedded for a material, and it never changes after upload,
// so a rewrite would mark the vector stale the same way an edit does for
// knowledge.
func documentVectorTextHash(summary string) string {
	sum := sha256.Sum256([]byte(summary))
	return hex.EncodeToString(sum[:])
}

// VectorTask is a knowledge item that needs an embedding.
type VectorTask struct {
	KnowledgeID int64
	Title       string
	Body        string
}

// KnowledgeVector is one stored, normalized embedding.
type KnowledgeVector struct {
	KnowledgeID int64
	Vector      []float32
}

func encodeVector(vector []float32) []byte {
	data := make([]byte, 4*len(vector))
	for i, value := range vector {
		binary.LittleEndian.PutUint32(data[4*i:], math.Float32bits(value))
	}
	return data
}

func decodeVector(data []byte, dimensions int) ([]float32, error) {
	if len(data) != 4*dimensions {
		return nil, fmt.Errorf("vector has %d bytes, expected %d", len(data), 4*dimensions)
	}
	vector := make([]float32, dimensions)
	for i := range vector {
		vector[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[4*i:]))
	}
	return vector, nil
}

// SaveKnowledgeVector stores or replaces the embedding of one knowledge item.
func (s *SQLite) SaveKnowledgeVector(ctx context.Context, task VectorTask, model string, vector []float32) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO knowledge_vectors
		(knowledge_item_id, model, dimensions, text_sha256, vector) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(knowledge_item_id) DO UPDATE SET model = excluded.model, dimensions = excluded.dimensions,
			text_sha256 = excluded.text_sha256, vector = excluded.vector, created_at = CURRENT_TIMESTAMP`,
		task.KnowledgeID, model, len(vector), vectorTextHash(task.Title, task.Body), encodeVector(vector))
	return err
}

// VectorTasks returns current knowledge whose vector is missing or stale for
// model and dimensions, oldest first. ids, when given, restricts the result to
// those items.
func (s *SQLite) VectorTasks(ctx context.Context, model string, dimensions, limit int, ids []int64) ([]VectorTask, int, error) {
	query := `SELECT k.id, k.title, k.body, v.model, v.dimensions, v.text_sha256
		FROM knowledge_items k LEFT JOIN knowledge_vectors v ON v.knowledge_item_id = k.id
		WHERE ` + currentKnowledgeClause
	args := make([]any, 0, len(ids))
	if len(ids) > 0 {
		query += " AND k.id IN (" + placeholders(len(ids)) + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := s.db.QueryContext(ctx, query+" ORDER BY k.id", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	tasks := make([]VectorTask, 0)
	pending := 0
	for rows.Next() {
		var task VectorTask
		var vectorModel, textHash *string
		var vectorDimensions *int
		if err := rows.Scan(&task.KnowledgeID, &task.Title, &task.Body, &vectorModel, &vectorDimensions, &textHash); err != nil {
			return nil, 0, err
		}
		if vectorModel != nil && *vectorModel == model && *vectorDimensions == dimensions && *textHash == vectorTextHash(task.Title, task.Body) {
			continue
		}
		pending++
		if limit <= 0 || len(tasks) < limit {
			tasks = append(tasks, task)
		}
	}
	return tasks, pending, rows.Err()
}

// CurrentVectors loads every up-to-date vector of current knowledge, in id
// order: de-duplication picks the best match by strict comparison, so the order
// must not depend on how the query happens to be planned.
func (s *SQLite) CurrentVectors(ctx context.Context, model string, dimensions int) ([]KnowledgeVector, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT k.id, k.title, k.body, v.text_sha256, v.vector
		FROM knowledge_items k JOIN knowledge_vectors v ON v.knowledge_item_id = k.id
		WHERE v.model = ? AND v.dimensions = ? AND `+currentKnowledgeClause+` ORDER BY k.id`, model, dimensions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	vectors := make([]KnowledgeVector, 0)
	for rows.Next() {
		var id int64
		var title, body, textHash string
		var data []byte
		if err := rows.Scan(&id, &title, &body, &textHash, &data); err != nil {
			return nil, err
		}
		if textHash != vectorTextHash(title, body) {
			continue
		}
		vector, err := decodeVector(data, dimensions)
		if err != nil {
			return nil, fmt.Errorf("knowledge %d: %w", id, err)
		}
		vectors = append(vectors, KnowledgeVector{KnowledgeID: id, Vector: vector})
	}
	return vectors, rows.Err()
}

// DocumentVectorTask is one material that needs an embedding, taken over its
// summary.
type DocumentVectorTask struct {
	DocumentID int64
	Summary    string
}

// DocumentVector is one stored, normalized material embedding.
type DocumentVector struct {
	DocumentID int64
	Vector     []float32
}

// SaveDocumentVector stores or replaces the embedding of one material.
func (s *SQLite) SaveDocumentVector(ctx context.Context, task DocumentVectorTask, model string, vector []float32) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO document_vectors
		(document_id, model, dimensions, text_sha256, vector) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(document_id) DO UPDATE SET model = excluded.model, dimensions = excluded.dimensions,
			text_sha256 = excluded.text_sha256, vector = excluded.vector, created_at = CURRENT_TIMESTAMP`,
		task.DocumentID, model, len(vector), documentVectorTextHash(task.Summary), encodeVector(vector))
	return err
}

// DocumentVectorTasks returns materials whose vector is missing or stale for
// model and dimensions, oldest first. Only materials that carry a summary are
// candidates -- there is nothing to embed otherwise.
func (s *SQLite) DocumentVectorTasks(ctx context.Context, model string, dimensions, limit int) ([]DocumentVectorTask, int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.summary, v.model, v.dimensions, v.text_sha256
		FROM documents d LEFT JOIN document_vectors v ON v.document_id = d.id
		WHERE d.summary <> '' ORDER BY d.id`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	tasks := make([]DocumentVectorTask, 0)
	pending := 0
	for rows.Next() {
		var task DocumentVectorTask
		var vectorModel, textHash *string
		var vectorDimensions *int
		if err := rows.Scan(&task.DocumentID, &task.Summary, &vectorModel, &vectorDimensions, &textHash); err != nil {
			return nil, 0, err
		}
		if vectorModel != nil && *vectorModel == model && *vectorDimensions == dimensions && *textHash == documentVectorTextHash(task.Summary) {
			continue
		}
		pending++
		if limit <= 0 || len(tasks) < limit {
			tasks = append(tasks, task)
		}
	}
	return tasks, pending, rows.Err()
}

// CurrentDocumentVectors loads every up-to-date material vector, in id order.
// Its text hash is checked against the stored summary so a rewritten summary
// drops out until the queue re-embeds it.
func (s *SQLite) CurrentDocumentVectors(ctx context.Context, model string, dimensions int) ([]DocumentVector, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.summary, v.text_sha256, v.vector
		FROM documents d JOIN document_vectors v ON v.document_id = d.id
		WHERE v.model = ? AND v.dimensions = ? AND d.summary <> '' ORDER BY d.id`, model, dimensions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	vectors := make([]DocumentVector, 0)
	for rows.Next() {
		var id int64
		var summary, textHash string
		var data []byte
		if err := rows.Scan(&id, &summary, &textHash, &data); err != nil {
			return nil, err
		}
		if textHash != documentVectorTextHash(summary) {
			continue
		}
		vector, err := decodeVector(data, dimensions)
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", id, err)
		}
		vectors = append(vectors, DocumentVector{DocumentID: id, Vector: vector})
	}
	return vectors, rows.Err()
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	result := make([]byte, 0, 2*count)
	for i := 0; i < count; i++ {
		if i > 0 {
			result = append(result, ',')
		}
		result = append(result, '?')
	}
	return string(result)
}
