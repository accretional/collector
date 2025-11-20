package collection

// DefaultSchema defines the minimal valid structure for a collection DB.
// Note: No metadata table. Metadata is assumed to be managed by the CollectionRepo.
const DefaultSchema = `
CREATE TABLE IF NOT EXISTS records (
    id TEXT PRIMARY KEY,
    proto_data BLOB NOT NULL,
    created_at INTEGER,
    updated_at INTEGER
);
`

const JSONSchema = `
ALTER TABLE records ADD COLUMN jsontext TEXT;
CREATE INDEX IF NOT EXISTS idx_records_json ON records(jsontext);
`

// FTSSchema uses the 'content' option to avoid duplicating data if possible, 
// or just indexes the jsontext.
const FTSSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS records_fts USING fts5(
    id UNINDEXED,
    content, 
    content='records', 
    content_rowid='rowid'
);
`

// VectorSchema for sqlite-vec
const VectorSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS records_vec USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[%d]
);
`
