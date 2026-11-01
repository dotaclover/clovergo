-- CloverGo schema baseline: one personal knowledge base.
--
-- This single file replaces the earlier 001–015 chain, squashed at a reset.
-- A material is one row in documents (the original file stays on disk, only
-- its path and whole-text summary are stored); a knowledge item is one row in
-- knowledge_items with its evidence quote inline. knowledge_fts and its
-- triggers are created by code (initFTS), because the tokenizer depends on
-- what the SQLite build supports.
--
-- Databases created by CloverGo v1 (which used schema_migrations) are refused
-- at startup instead of being migrated, and so is any database recording a
-- schema version this build does not know.

CREATE TABLE documents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    filename TEXT NOT NULL,
    source_key TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    sha256 TEXT NOT NULL,
    media_type TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT '',
    -- A human-named shelf (日语, PHP, F1 …), stored verbatim; empty = unfiled.
    topic TEXT NOT NULL DEFAULT '',
    -- The model's summary of the whole material, written at upload.
    summary TEXT NOT NULL DEFAULT '',
    stored_path TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'ready'
        CHECK (status IN ('ready', 'processing', 'published', 'processed_empty', 'analysis_failed')),
    error_message TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    useful_count INTEGER NOT NULL DEFAULT 0,
    -- How many drafts were written from this material's original text.
    use_count INTEGER NOT NULL DEFAULT 0,
    processing_run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TEXT,
    UNIQUE(sha256),
    UNIQUE(source_key, version)
);
CREATE INDEX idx_documents_created ON documents(created_at DESC, id DESC);
CREATE INDEX idx_documents_status ON documents(status, id DESC);
CREATE INDEX idx_documents_topic ON documents(topic, id DESC);

CREATE TABLE knowledge_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    document_id INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    tags_json TEXT NOT NULL DEFAULT '[]',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    fingerprint TEXT NOT NULL,
    -- A contiguous slice of the material's normalized text; the rune offsets
    -- are recomputable from the stored file.
    evidence_quote TEXT NOT NULL,
    quote_start INTEGER NOT NULL,
    quote_end INTEGER NOT NULL,
    read_count INTEGER NOT NULL DEFAULT 0,
    -- How many drafts were written from this item.
    use_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_knowledge_fingerprint ON knowledge_items(fingerprint);
CREATE INDEX idx_knowledge_created ON knowledge_items(created_at DESC, id DESC);
CREATE INDEX idx_knowledge_kind ON knowledge_items(kind, created_at DESC);
CREATE INDEX idx_knowledge_document ON knowledge_items(document_id, id);

-- One embedding per knowledge item (title + body) and per material (its
-- summary). A vector is current only when its model, dimensions, and text hash
-- match the configured model and the text it was built from.
CREATE TABLE knowledge_vectors (
    knowledge_item_id INTEGER PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL CHECK (dimensions > 0),
    text_sha256 TEXT NOT NULL,
    vector BLOB NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE document_vectors (
    document_id INTEGER PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL CHECK (dimensions > 0),
    text_sha256 TEXT NOT NULL,
    vector BLOB NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE agent_runs (
    run_id TEXT PRIMARY KEY,
    purpose TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    latency_ms INTEGER NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER,
    output_tokens INTEGER,
    total_tokens INTEGER,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TEXT
);
CREATE INDEX idx_agent_runs_created_at ON agent_runs(created_at DESC);

CREATE TABLE agent_run_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL REFERENCES agent_runs(run_id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('succeeded', 'failed')),
    latency_ms INTEGER NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    details_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(run_id, ordinal)
);

CREATE TABLE usage_ledger (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    purpose TEXT NOT NULL,
    model TEXT NOT NULL,
    input_tokens INTEGER,
    output_tokens INTEGER,
    total_tokens INTEGER,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_usage_ledger_run ON usage_ledger(run_id);

-- Each answered agent question, for read-back on the Q&A screen. Nothing in
-- retrieval, extraction or drafting reads it.
CREATE TABLE agent_qa_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    question TEXT NOT NULL,
    answer TEXT NOT NULL,
    citations_json TEXT NOT NULL DEFAULT '[]',
    steps_json TEXT NOT NULL DEFAULT '[]',
    model TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_agent_qa_history_recent ON agent_qa_history(created_at DESC, id DESC);

CREATE TABLE workflow_runs (
    run_id TEXT PRIMARY KEY REFERENCES agent_runs(run_id) ON DELETE CASCADE,
    workflow_key TEXT NOT NULL,
    workflow_version TEXT NOT NULL,
    input_json TEXT NOT NULL,
    input_sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_workflow_runs_created ON workflow_runs(created_at DESC);

CREATE TABLE artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL REFERENCES workflow_runs(run_id) ON DELETE RESTRICT,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    kind TEXT NOT NULL,
    schema_version TEXT NOT NULL,
    title TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    payload_sha256 TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(run_id, ordinal)
);
CREATE INDEX idx_artifacts_kind_created ON artifacts(kind, created_at DESC);

-- A draft row is created as soon as the request is accepted (status running)
-- and filled in by the background queue. wechat_media_id records the copy in
-- the Official Account draft box; a push never publishes.
CREATE TABLE media_drafts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL UNIQUE REFERENCES workflow_runs(run_id) ON DELETE RESTRICT,
    artifact_id INTEGER NOT NULL UNIQUE REFERENCES artifacts(id) ON DELETE RESTRICT,
    platform TEXT NOT NULL,
    format TEXT NOT NULL CHECK (format IN ('article', 'gallery')),
    topic TEXT NOT NULL,
    angle TEXT NOT NULL DEFAULT '',
    audience TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    wechat_media_id TEXT NOT NULL DEFAULT '',
    wechat_pushed_at TEXT,
    status TEXT NOT NULL DEFAULT 'generated',
    error_message TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_media_drafts_created ON media_drafts(created_at DESC, id DESC);
CREATE INDEX idx_media_drafts_platform ON media_drafts(platform, created_at DESC);

-- The newest revision of a platform is in effect; without rows the built-in
-- default applies (revision 0).
CREATE TABLE platform_style_revisions (
    platform_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    settings_json TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (platform_id, revision)
);

-- The owner's replacement for one built-in prompt layer. The built-in text is
-- embedded in the binary; deleting the row restores it.
CREATE TABLE prompt_overrides (
    layer TEXT PRIMARY KEY,
    text TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Official Account credentials and push defaults, edited on the settings
-- screen. A missing row means the environment seed (WECHAT_*) is in effect.
-- proxy_url is an optional socks5:// URL that gives the push a stable egress IP.
CREATE TABLE wechat_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    app_id TEXT NOT NULL DEFAULT '',
    app_secret TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    thumb_media_id TEXT NOT NULL DEFAULT '',
    proxy_url TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
