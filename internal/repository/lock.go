package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"clovergo/internal/domain"
)

// AppLock reads the global password row; nil means no password is set.
func (s *SQLite) AppLock(ctx context.Context) (*domain.AppLock, error) {
	var lock domain.AppLock
	err := s.db.QueryRowContext(ctx, `SELECT password_hash, salt, iterations, updated_at FROM app_lock WHERE id = 1`).
		Scan(&lock.PasswordHash, &lock.Salt, &lock.Iterations, &lock.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read app lock: %w", err)
	}
	return &lock, nil
}

// SaveAppLock sets or replaces the global password.
func (s *SQLite) SaveAppLock(ctx context.Context, lock domain.AppLock) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO app_lock (id, password_hash, salt, iterations, updated_at)
		VALUES (1, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			password_hash = excluded.password_hash,
			salt = excluded.salt,
			iterations = excluded.iterations,
			updated_at = excluded.updated_at`,
		lock.PasswordHash, lock.Salt, lock.Iterations); err != nil {
		return fmt.Errorf("save app lock: %w", err)
	}
	return nil
}

// DeleteAppLock removes the global password.
func (s *SQLite) DeleteAppLock(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM app_lock WHERE id = 1`); err != nil {
		return fmt.Errorf("delete app lock: %w", err)
	}
	return nil
}
