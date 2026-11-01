-- A knowledge fingerprint is unique within one material, not across the
-- library: the same claim from two materials (or from a new version of the
-- same source) is two knowledge items, each with its own evidence.
DROP INDEX IF EXISTS idx_knowledge_fingerprint;
CREATE UNIQUE INDEX idx_knowledge_fingerprint ON knowledge_items(document_id, fingerprint);
