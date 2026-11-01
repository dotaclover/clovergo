package repository

import (
	"context"
	"database/sql"
	"errors"

	"clovergo/internal/domain"
)

const rootSelect = `SELECT r.id, r.path, r.name, r.created_at,
	(SELECT COUNT(*) FROM documents d WHERE d.root_id = r.id)
	FROM library_roots r`

func scanRoot(row scanner) (*domain.LibraryRoot, error) {
	var root domain.LibraryRoot
	err := row.Scan(&root.ID, &root.Path, &root.Name, &root.CreatedAt, &root.DocumentCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &root, err
}

func (s *SQLite) AddRoot(ctx context.Context, path, name string) (*domain.LibraryRoot, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO library_roots (path, name) VALUES (?, ?)`, path, name)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetRoot(ctx, id)
}

func (s *SQLite) GetRoot(ctx context.Context, id int64) (*domain.LibraryRoot, error) {
	return scanRoot(s.db.QueryRowContext(ctx, rootSelect+" WHERE r.id = ?", id))
}

func (s *SQLite) ListRoots(ctx context.Context) ([]domain.LibraryRoot, error) {
	rows, err := s.db.QueryContext(ctx, rootSelect+" ORDER BY r.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roots := make([]domain.LibraryRoot, 0)
	for rows.Next() {
		root, err := scanRoot(rows)
		if err != nil {
			return nil, err
		}
		roots = append(roots, *root)
	}
	return roots, rows.Err()
}

// DeleteRoot forgets a folder. Its documents stay in the library, detached from
// the folder.
func (s *SQLite) DeleteRoot(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE documents SET root_id = 0, missing = 0 WHERE root_id = ?`, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM library_roots WHERE id = ?`, id)
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
	return tx.Commit()
}

func (s *SQLite) ListRootDocuments(ctx context.Context, rootID int64) ([]domain.Document, error) {
	rows, err := s.db.QueryContext(ctx, documentSelect+" WHERE d.root_id = ? ORDER BY d.id", rootID)
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

// MarkRootMissing replaces the "file no longer on disk" flags of one folder:
// everything is cleared, then every version of each listed relative path is
// flagged.
func (s *SQLite) MarkRootMissing(ctx context.Context, rootID int64, missingRelPaths []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE documents SET missing = 0 WHERE root_id = ? AND missing <> 0`, rootID); err != nil {
		return err
	}
	for _, rel := range missingRelPaths {
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET missing = 1 WHERE root_id = ? AND rel_path = ?`, rootID, rel); err != nil {
			return err
		}
	}
	return tx.Commit()
}
