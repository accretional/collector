package collection

// DefaultSchema is the core table for storing collection records.
const DefaultSchema = `
CREATE TABLE IF NOT EXISTS records (
    id TEXT PRIMARY KEY,
    proto_data BLOB,
    data_uri TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    labels TEXT
);
`

// JSONSchema adds a JSON text column for efficient querying of unstructured data.
const JSONSchema = `
ALTER TABLE records ADD COLUMN jsontext TEXT;
`

// FTSSchema creates a virtual table for full-text search.
const FTSSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS records_fts USING fts5(
    content,
    content_rowid=rowid,
    tokenize = "porter unicode61"
);
`
