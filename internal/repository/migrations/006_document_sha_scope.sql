-- The same bytes may legitimately exist under two sources (two folders, two
-- filenames) or come back as a later version of one source (A -> B -> A), so
-- documents.sha256 is no longer globally unique. "Unchanged / duplicate" is
-- decided in code: the newest version of the same source_key has the same
-- sha256. SQLite cannot drop a table constraint, so the table is rebuilt; the
-- migration runner turns foreign keys off for this so the CASCADE references
-- from knowledge_items and document_vectors are neither fired nor rewritten.

CREATE TABLE documents_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    filename TEXT NOT NULL,
    source_key TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    sha256 TEXT NOT NULL,
    media_type TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT '',
    topic TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    stored_path TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'ready'
        CHECK (status IN ('ready', 'processing', 'published', 'processed_empty', 'analysis_failed')),
    error_message TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    useful_count INTEGER NOT NULL DEFAULT 0,
    use_count INTEGER NOT NULL DEFAULT 0,
    processing_run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TEXT,
    root_id INTEGER NOT NULL DEFAULT 0,
    rel_path TEXT NOT NULL DEFAULT '',
    missing INTEGER NOT NULL DEFAULT 0,
    origin_size INTEGER NOT NULL DEFAULT 0,
    origin_mtime INTEGER NOT NULL DEFAULT 0,
    UNIQUE(source_key, version)
);

INSERT INTO documents_new (id, filename, source_key, version, sha256, media_type, language, topic,
    summary, stored_path, status, error_message, attempts, useful_count, use_count, processing_run_id,
    created_at, processed_at, root_id, rel_path, missing, origin_size, origin_mtime)
SELECT id, filename, source_key, version, sha256, media_type, language, topic,
    summary, stored_path, status, error_message, attempts, useful_count, use_count, processing_run_id,
    created_at, processed_at, root_id, rel_path, missing, origin_size, origin_mtime
FROM documents;

DROP TABLE documents;
ALTER TABLE documents_new RENAME TO documents;

CREATE INDEX idx_documents_created ON documents(created_at DESC, id DESC);
CREATE INDEX idx_documents_status ON documents(status, id DESC);
CREATE INDEX idx_documents_topic ON documents(topic, id DESC);
CREATE INDEX idx_documents_root ON documents(root_id, rel_path);
CREATE INDEX idx_documents_source_sha ON documents(source_key, sha256);
