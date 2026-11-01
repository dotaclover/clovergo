package repository

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"clovergo/internal/domain"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type SQLite struct {
	db      *sql.DB
	ftsMode string
}

func OpenSQLite(path string) (*SQLite, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(abs) +
		"?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)

	store := &SQLite{db: db, ftsMode: "like"}
	if err := store.Ping(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.initFTS(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLite) Close() error                   { return s.db.Close() }
func (s *SQLite) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *SQLite) FTSMode() string                { return s.ftsMode }

// RecoveredWork counts records left mid-flight by a process that stopped
// without finishing them.
type RecoveredWork struct {
	Documents int64
	Runs      int64
	Drafts    int64
}

// RecoverInterruptedWork must run before the HTTP server starts. CloverGo is a
// single process per database, so anything still marked processing/running at
// startup belongs to a previous process that can no longer finish it.
//
// A document caught mid-extraction goes back to 'ready', because extraction
// runs on the whole material in one call, so there is nothing partial to keep.
// The owner starts it again from the materials list. It only becomes
// analysis_failed once it has used up MaxDocumentAttempts, so a poison material
// cannot loop forever.
func (s *SQLite) RecoverInterruptedWork(ctx context.Context) (RecoveredWork, error) {
	const message = "interrupted because the previous CloverGo process stopped before finishing"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RecoveredWork{}, err
	}
	defer tx.Rollback()
	var recovered RecoveredWork
	result, err := tx.ExecContext(ctx, `UPDATE documents
		SET status = CASE WHEN attempts + 1 >= ? THEN 'analysis_failed' ELSE 'ready' END,
			error_message = CASE WHEN attempts + 1 >= ? THEN ? ELSE '' END,
			attempts = attempts + 1,
			processing_run_id = ''
		WHERE status = 'processing'`, domain.MaxDocumentAttempts, domain.MaxDocumentAttempts, message)
	if err != nil {
		return RecoveredWork{}, fmt.Errorf("recover documents: %w", err)
	}
	if recovered.Documents, err = result.RowsAffected(); err != nil {
		return RecoveredWork{}, err
	}
	result, err = tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'failed', error_code = 'RUN_INTERRUPTED',
		error_message = ?, completed_at = CURRENT_TIMESTAMP WHERE status = 'running'`, message)
	if err != nil {
		return RecoveredWork{}, fmt.Errorf("recover agent runs: %w", err)
	}
	if recovered.Runs, err = result.RowsAffected(); err != nil {
		return RecoveredWork{}, err
	}
	// A draft being written by the previous process can never finish; it is
	// shown as failed instead of spinning forever.
	result, err = tx.ExecContext(ctx, `UPDATE media_drafts SET status = 'failed', error_message = ?
		WHERE status = 'running'`, message)
	if err != nil {
		return RecoveredWork{}, fmt.Errorf("recover media drafts: %w", err)
	}
	if recovered.Drafts, err = result.RowsAffected(); err != nil {
		return RecoveredWork{}, err
	}
	return recovered, tx.Commit()
}

// ErrLegacyDatabase means the file was created by CloverGo v1. v2 starts from
// a new baseline schema and deliberately does not migrate v1 data.
var ErrLegacyDatabase = errors.New("this database was created by CloverGo v1; move the database file (and its -wal/-shm files) away and re-import the materials")

// ErrIncompatibleSchema means the file records a schema version this build
// does not know. The single-library baseline replaced the earlier baseline
// wholesale, so such a file is refused rather than migrated: move it away and
// let the app create a fresh one.
var ErrIncompatibleSchema = errors.New("this database was created by an older CloverGo schema; move the database file (and its -wal/-shm files) away and re-import the materials")

var migrations = []struct {
	version int
	path    string
}{
	// The 001–015 chain was squashed into this one baseline at a reset. A
	// database from before the squash records versions this list no longer
	// knows and is refused with ErrIncompatibleSchema; new migrations take 7
	// onward (001–006 are registered below).
	{version: 1, path: "migrations/001_baseline.sql"},
	{version: 2, path: "migrations/002_knowledge_fingerprint_scope.sql"},
	{version: 3, path: "migrations/003_app_lock.sql"},
	{version: 4, path: "migrations/004_library_roots.sql"},
	{version: 5, path: "migrations/005_wechat_publish.sql"},
	{version: 6, path: "migrations/006_document_sha_scope.sql"},
}

func (s *SQLite) migrate(ctx context.Context) error {
	var legacy int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'").Scan(&legacy); err != nil {
		return fmt.Errorf("inspect schema: %w", err)
	}
	if legacy > 0 {
		return ErrLegacyDatabase
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS clovergo_schema (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create clovergo_schema: %w", err)
	}
	known := make(map[int]bool, len(migrations))
	for _, migration := range migrations {
		known[migration.version] = true
	}
	rows, err := s.db.QueryContext(ctx, "SELECT version FROM clovergo_schema")
	if err != nil {
		return fmt.Errorf("read schema versions: %w", err)
	}
	recorded := make([]int, 0, len(migrations))
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return fmt.Errorf("read schema version: %w", err)
		}
		recorded = append(recorded, version)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read schema versions: %w", err)
	}
	for _, version := range recorded {
		if !known[version] {
			return fmt.Errorf("%w (recorded schema version %d)", ErrIncompatibleSchema, version)
		}
	}
	for _, migration := range migrations {
		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM clovergo_schema WHERE version = ?", migration.version).Scan(&count); err != nil {
			return fmt.Errorf("read schema version %d: %w", migration.version, err)
		}
		if count > 0 {
			continue
		}
		schema, err := migrationFiles.ReadFile(migration.path)
		if err != nil {
			return fmt.Errorf("read embedded migration %d: %w", migration.version, err)
		}
		if err := s.applyMigration(ctx, migration.version, string(schema)); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration runs one migration on a pinned connection with foreign keys
// off, because rebuilding a parent table (DROP + RENAME) would otherwise fire
// ON DELETE CASCADE on its children. foreign_key_check runs before commit so
// a rebuild cannot leave dangling references behind.
func (s *SQLite) applyMigration(ctx context.Context, version int, schema string) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open migration connection %d: %w", version, err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign keys for migration %d: %w", version, err)
	}
	defer conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", version, err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply migration %d: %w", version, err)
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("check foreign keys after migration %d: %w", version, err)
	}
	broken := rows.Next()
	rows.Close()
	if broken {
		return fmt.Errorf("migration %d left broken foreign key references", version)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO clovergo_schema(version) VALUES (?)", version); err != nil {
		return fmt.Errorf("record migration %d: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", version, err)
	}
	return nil
}

// initFTS selects the best available full-text index. When this SQLite build
// has no FTS5 at all the mode is "like" and is reported by /health/ready; any
// other failure to create or maintain the index is a startup error so search
// can never silently drift away from knowledge_items.
func (s *SQLite) initFTS(ctx context.Context) error {
	var definition string
	err := s.db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'knowledge_fts'").Scan(&definition)
	if err == nil {
		if strings.Contains(strings.ToLower(definition), "trigram") {
			s.ftsMode = "fts5-trigram"
		} else {
			s.ftsMode = "fts5-unicode61"
		}
		if err := s.createFTSTriggers(ctx); err != nil {
			return fmt.Errorf("create knowledge_fts triggers: %w", err)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("inspect knowledge_fts: %w", err)
	}

	if _, err = s.db.ExecContext(ctx, `CREATE VIRTUAL TABLE knowledge_fts USING fts5(
		title, body, content='knowledge_items', content_rowid='id', tokenize='trigram'
	)`); err == nil {
		s.ftsMode = "fts5-trigram"
	} else if _, fallbackErr := s.db.ExecContext(ctx, `CREATE VIRTUAL TABLE knowledge_fts USING fts5(
		title, body, content='knowledge_items', content_rowid='id', tokenize='unicode61 remove_diacritics 2'
	)`); fallbackErr == nil {
		s.ftsMode = "fts5-unicode61"
	} else {
		s.ftsMode = "like"
		return nil
	}
	if err := s.createFTSTriggers(ctx); err != nil {
		return fmt.Errorf("create knowledge_fts triggers: %w", err)
	}
	// knowledge_fts is an external-content table: selecting its rowids reads
	// knowledge_items, so existing rows must be indexed with 'rebuild'.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO knowledge_fts(knowledge_fts) VALUES ('rebuild')`); err != nil {
		return fmt.Errorf("backfill knowledge_fts: %w", err)
	}
	return nil
}

func (s *SQLite) createFTSTriggers(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TRIGGER IF NOT EXISTS knowledge_ai AFTER INSERT ON knowledge_items BEGIN
			INSERT INTO knowledge_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
		END;
		CREATE TRIGGER IF NOT EXISTS knowledge_ad AFTER DELETE ON knowledge_items BEGIN
			INSERT INTO knowledge_fts(knowledge_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
		END;
		DROP TRIGGER IF EXISTS knowledge_au;
		CREATE TRIGGER knowledge_au AFTER UPDATE OF title, body ON knowledge_items BEGIN
			INSERT INTO knowledge_fts(knowledge_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
			INSERT INTO knowledge_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
		END;
	`)
	return err
}
