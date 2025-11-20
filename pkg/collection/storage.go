package collection

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "time"

    pb "github.com/accretional/collector/gen/collector"
    _ "modernc.org/sqlite"
)

// Checkpoint forces a WAL checkpoint (useful for testing durability)
func (c *Collection) Checkpoint() error {
    _, err := c.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
    return err
}

// initDatabase creates the SQLite tables and indexes
func (c *Collection) initDatabase() error {
    db, err := sql.Open("sqlite", c.dbPath)
    if err != nil {
        return fmt.Errorf("failed to open database: %w", err)
    }
    c.db = db

    // Enable foreign keys and WAL mode for better concurrency
    pragmas := []string{
        "PRAGMA foreign_keys = ON",
        "PRAGMA journal_mode = WAL",
        "PRAGMA synchronous = NORMAL",
    }
    for _, pragma := range pragmas {
        if _, err := db.Exec(pragma); err != nil {
            return fmt.Errorf("failed to set pragma: %w", err)
        }
    }

    // Create metadata table
    if _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS collection_metadata (
            namespace TEXT NOT NULL,
            name TEXT NOT NULL,
            message_type_namespace TEXT,
            message_type_name TEXT,
            indexed_fields TEXT, -- JSON array
            server_endpoint TEXT,
            created_at INTEGER NOT NULL,
            updated_at INTEGER NOT NULL,
            labels TEXT -- JSON object
        )
    `); err != nil {
        return fmt.Errorf("failed to create metadata table: %w", err)
    }

    // Create records table
    // Design notes:
    // - proto_data: Binary protobuf data (BLOB) - the canonical source of truth
    // - jsontext: JSON-encoded representation of proto_data (TEXT) - for querying/searching
    //   This is generated from proto_data and kept in sync. SQLite's JSON functions
    //   (json_extract, etc.) work on this column, not on proto_data.
    // - labels: User-controlled key-value metadata (JSON object) - separate from proto_data
    //   Users can set arbitrary labels for filtering/organizing records independently
    //   of the protobuf message content.
    // - data_uri: Optional reference to external/filesystem data associated with this record
    if _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS records (
            id TEXT PRIMARY KEY,
            proto_data BLOB NOT NULL,
            jsontext TEXT NOT NULL,
            data_uri TEXT,
            created_at INTEGER NOT NULL,
            updated_at INTEGER NOT NULL,
            labels TEXT
        )
    `); err != nil {
        return fmt.Errorf("failed to create records table: %w", err)
    }

    // Create indexes
    indexes := []string{
        "CREATE INDEX IF NOT EXISTS idx_records_created_at ON records(created_at)",
        "CREATE INDEX IF NOT EXISTS idx_records_updated_at ON records(updated_at)",
    }
    for _, idx := range indexes {
        if _, err := db.Exec(idx); err != nil {
            return fmt.Errorf("failed to create index: %w", err)
        }
    }

    // Create FTS5 virtual table for full-text search
    // Uses jsontext column content for indexing, allowing full-text search
    // across the JSON representation of protobuf messages
    if _, err := db.Exec(`
        CREATE VIRTUAL TABLE IF NOT EXISTS records_fts USING fts5(
            id UNINDEXED,
            content,
            content='records',
            content_rowid='rowid'
        )
    `); err != nil {
        return fmt.Errorf("failed to create FTS table: %w", err)
    }

    return nil
}

// saveMetadata stores collection metadata in the database
func (c *Collection) saveMetadata() error {
    indexedFieldsJSON, _ := json.Marshal(c.proto.IndexedFields)
    var labelsJSON []byte
    if c.proto.Metadata != nil && c.proto.Metadata.Labels != nil {
        labelsJSON, _ = json.Marshal(c.proto.Metadata.Labels)
    }

    var messageTypeNS, messageTypeName string
    if c.proto.MessageType != nil {
        messageTypeNS = c.proto.MessageType.Namespace
        messageTypeName = c.proto.MessageType.MessageName
    }

    _, err := c.db.Exec(`
        INSERT OR REPLACE INTO collection_metadata 
        (namespace, name, message_type_namespace, message_type_name, 
         indexed_fields, server_endpoint, created_at, updated_at, labels)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `,
        c.proto.Namespace,
        c.proto.Name,
        messageTypeNS,
        messageTypeName,
        string(indexedFieldsJSON),
        c.proto.ServerEndpoint,
        c.proto.Metadata.CreatedAt.Seconds,
        c.proto.Metadata.UpdatedAt.Seconds,
        string(labelsJSON),
    )

    return err
}

// loadMetadata loads collection metadata from the database
func (c *Collection) loadMetadata() (*pb.Collection, error) {
    var (
        namespace, name                   string
        messageTypeNS, messageTypeName    sql.NullString
        indexedFieldsJSON, labelsJSON     string
        serverEndpoint                    sql.NullString
        createdAt, updatedAt              int64
    )

    err := c.db.QueryRow(`
        SELECT namespace, name, message_type_namespace, message_type_name,
               indexed_fields, server_endpoint, created_at, updated_at, labels
        FROM collection_metadata
        LIMIT 1
    `).Scan(
        &namespace, &name, &messageTypeNS, &messageTypeName,
        &indexedFieldsJSON, &serverEndpoint, &createdAt, &updatedAt, &labelsJSON,
    )

    if err != nil {
        return nil, fmt.Errorf("failed to load metadata: %w", err)
    }

    proto := &pb.Collection{
        Namespace: namespace,
        Name:      name,
        Metadata: &pb.Metadata{
            CreatedAt: &pb.Timestamp{Seconds: createdAt},
            UpdatedAt: &pb.Timestamp{Seconds: updatedAt},
        },
    }

    if messageTypeNS.Valid && messageTypeName.Valid {
        proto.MessageType = &pb.MessageTypeRef{
            Namespace:   messageTypeNS.String,
            MessageName: messageTypeName.String,
        }
    }

    if indexedFieldsJSON != "" {
        json.Unmarshal([]byte(indexedFieldsJSON), &proto.IndexedFields)
    }

    if serverEndpoint.Valid {
        proto.ServerEndpoint = serverEndpoint.String
    }

    if labelsJSON != "" {
        labels := make(map[string]string)
        if err := json.Unmarshal([]byte(labelsJSON), &labels); err == nil {
            proto.Metadata.Labels = labels
        }
    }

    return proto, nil
}

// protoToJSON converts proto_data bytes to JSON text
// This assumes proto_data is already JSON or converts it if needed
func protoToJSON(protoData []byte) (string, error) {
    // Check if it's already valid JSON
    var js json.RawMessage
    if err := json.Unmarshal(protoData, &js); err == nil {
        // Already JSON, compact it
        compact := &bytes.Buffer{}
        if err := json.Compact(compact, protoData); err == nil {
            return compact.String(), nil
        }
        return string(protoData), nil
    }
    
    // If not JSON, try to decode as protobuf and marshal to JSON
    // For now, we'll just store the bytes as a base64-encoded string in a JSON object
    // In a real implementation, you'd use protojson to convert the proto to JSON
    fallback := map[string]interface{}{
        "_raw": string(protoData),
    }
    jsonBytes, err := json.Marshal(fallback)
    if err != nil {
        return "", fmt.Errorf("failed to convert proto to JSON: %w", err)
    }
    return string(jsonBytes), nil
}

// CreateRecord adds a new record to the collection
func (c *Collection) CreateRecord(record *pb.CollectionRecord) error {
    if record.Id == "" {
        return fmt.Errorf("record id is required")
    }

    now := time.Now().Unix()
    if record.Metadata == nil {
        record.Metadata = &pb.Metadata{
            CreatedAt: &pb.Timestamp{Seconds: now},
            UpdatedAt: &pb.Timestamp{Seconds: now},
        }
    }

    // Convert proto_data to JSON text for querying
    jsontext, err := protoToJSON(record.ProtoData)
    if err != nil {
        return fmt.Errorf("failed to convert proto to JSON: %w", err)
    }

    // User-controlled labels (separate from proto content)
    labelsJSON, _ := json.Marshal(record.Metadata.Labels)

    _, err = c.db.Exec(`
        INSERT INTO records (id, proto_data, jsontext, data_uri, created_at, updated_at, labels)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `,
        record.Id,
        record.ProtoData,
        jsontext,
        record.DataUri,
        record.Metadata.CreatedAt.Seconds,
        record.Metadata.UpdatedAt.Seconds,
        string(labelsJSON),
    )

    if err != nil {
        return err
    }

    // Update FTS index using jsontext
    if err := c.updateFTSIndex(record.Id, jsontext); err != nil {
        return fmt.Errorf("failed to update FTS index: %w", err)
    }

    return nil
}

// GetRecord retrieves a record by ID
func (c *Collection) GetRecord(id string) (*pb.CollectionRecord, error) {
    var (
        protoData           []byte
        jsontext            string
        dataUri             sql.NullString
        createdAt, updatedAt int64
        labelsJSON          string
    )

    err := c.db.QueryRow(`
        SELECT proto_data, jsontext, data_uri, created_at, updated_at, labels
        FROM records
        WHERE id = ?
    `, id).Scan(&protoData, &jsontext, &dataUri, &createdAt, &updatedAt, &labelsJSON)

    if err == sql.ErrNoRows {
        return nil, fmt.Errorf("record not found: %s", id)
    }
    if err != nil {
        return nil, err
    }

    record := &pb.CollectionRecord{
        Id:        id,
        ProtoData: protoData,
        Metadata: &pb.Metadata{
            CreatedAt: &pb.Timestamp{Seconds: createdAt},
            UpdatedAt: &pb.Timestamp{Seconds: updatedAt},
        },
    }

    if dataUri.Valid {
        record.DataUri = dataUri.String
    }

    if labelsJSON != "" {
        labels := make(map[string]string)
        if err := json.Unmarshal([]byte(labelsJSON), &labels); err == nil {
            record.Metadata.Labels = labels
        }
    }

    return record, nil
}

// UpdateRecord updates an existing record
func (c *Collection) UpdateRecord(record *pb.CollectionRecord) error {
    if record.Id == "" {
        return fmt.Errorf("record id is required")
    }

    now := time.Now().Unix()
    if record.Metadata == nil {
        record.Metadata = &pb.Metadata{}
    }
    record.Metadata.UpdatedAt = &pb.Timestamp{Seconds: now}

    // Convert proto_data to JSON text for querying
    jsontext, err := protoToJSON(record.ProtoData)
    if err != nil {
        return fmt.Errorf("failed to convert proto to JSON: %w", err)
    }

    labelsJSON, _ := json.Marshal(record.Metadata.Labels)

    result, err := c.db.Exec(`
        UPDATE records
        SET proto_data = ?, jsontext = ?, data_uri = ?, updated_at = ?, labels = ?
        WHERE id = ?
    `,
        record.ProtoData,
        jsontext,
        record.DataUri,
        record.Metadata.UpdatedAt.Seconds,
        string(labelsJSON),
        record.Id,
    )

    if err != nil {
        return err
    }

    rows, err := result.RowsAffected()
    if err != nil {
        return err
    }
    if rows == 0 {
        return fmt.Errorf("record not found: %s", record.Id)
    }

    // Update FTS index using jsontext
    if err := c.updateFTSIndex(record.Id, jsontext); err != nil {
        return fmt.Errorf("failed to update FTS index: %w", err)
    }

    return nil
}

// DeleteRecord removes a record by ID
func (c *Collection) DeleteRecord(id string) error {
    result, err := c.db.Exec("DELETE FROM records WHERE id = ?", id)
    if err != nil {
        return err
    }

    rows, err := result.RowsAffected()
    if err != nil {
        return err
    }
    if rows == 0 {
        return fmt.Errorf("record not found: %s", id)
    }

    // Delete from FTS index
    if err := c.deleteFTSIndex(id); err != nil {
        return fmt.Errorf("failed to delete from FTS index: %w", err)
    }

    return nil
}

// ListRecords returns paginated records
func (c *Collection) ListRecords(offset, limit int) ([]*pb.CollectionRecord, error) {
    if limit <= 0 {
        limit = 100
    }

    rows, err := c.db.Query(`
        SELECT id, proto_data, jsontext, data_uri, created_at, updated_at, labels
        FROM records
        ORDER BY created_at DESC
        LIMIT ? OFFSET ?
    `, limit, offset)

    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var records []*pb.CollectionRecord
    for rows.Next() {
        var (
            id                  string
            protoData           []byte
            jsontext            string
            dataUri             sql.NullString
            createdAt, updatedAt int64
            labelsJSON          string
        )

        if err := rows.Scan(&id, &protoData, &jsontext, &dataUri, &createdAt, &updatedAt, &labelsJSON); err != nil {
            return nil, err
        }

        record := &pb.CollectionRecord{
            Id:        id,
            ProtoData: protoData,
            Metadata: &pb.Metadata{
                CreatedAt: &pb.Timestamp{Seconds: createdAt},
                UpdatedAt: &pb.Timestamp{Seconds: updatedAt},
            },
        }

        if dataUri.Valid {
            record.DataUri = dataUri.String
        }

        if labelsJSON != "" {
            labels := make(map[string]string)
            if err := json.Unmarshal([]byte(labelsJSON), &labels); err == nil {
                record.Metadata.Labels = labels
            }
        }

        records = append(records, record)
    }

    return records, rows.Err()
}

// CountRecords returns the total number of records
func (c *Collection) CountRecords() (int64, error) {
    var count int64
    err := c.db.QueryRow("SELECT COUNT(*) FROM records").Scan(&count)
    return count, err
}
