package collection

// DefaultSchema defines the minimal valid structure for a collection DB.
// It focuses purely on record storage. Metadata is managed by the Collection object/Repo.
const DefaultSchema = `
CREATE TABLE IF NOT EXISTS records (
    id TEXT PRIMARY KEY,
    proto_data BLOB NOT NULL,
    data_uri TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    labels TEXT -- JSON object for lightweight labeling
);
CREATE INDEX IF NOT EXISTS idx_records_created_at ON records(created_at);
CREATE INDEX IF NOT EXISTS idx_records_updated_at ON records(updated_at);
`

// JSONSchema adds a generated column for JSON operations.
const JSONSchema = `
ALTER TABLE records ADD COLUMN jsontext TEXT;
CREATE INDEX IF NOT EXISTS idx_records_json ON records(jsontext);
`

// FTSSchema uses the 'content' option to avoid duplicating data if possible.
const FTSSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS records_fts USING fts5(
    id UNINDEXED,
    content, 
    content='records', 
    content_rowid='rowid'
);
`

// VectorSchema for sqlite-vec extension.
const VectorSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS records_vec USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[%d]
);
`
