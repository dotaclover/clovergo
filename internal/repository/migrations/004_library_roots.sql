-- Library folders: directories the owner registered for scanning. A document
-- imported from one remembers its root and relative path, plus the size and
-- mtime it had then, so a rescan can skip files that did not change.
-- root_id 0 means the material was uploaded or pasted rather than scanned.
CREATE TABLE library_roots (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    path       TEXT    NOT NULL UNIQUE,
    name       TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE documents ADD COLUMN root_id      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE documents ADD COLUMN rel_path     TEXT    NOT NULL DEFAULT '';
ALTER TABLE documents ADD COLUMN missing      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE documents ADD COLUMN origin_size  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE documents ADD COLUMN origin_mtime INTEGER NOT NULL DEFAULT 0;

CREATE INDEX idx_documents_root ON documents(root_id, rel_path);
