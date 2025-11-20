package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/accretional/collector/pkg/collection"
	pb "github.com/accretional/collector/gen/collector"
	
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite" 
)

type SqliteStore struct {
	db      *sql.DB
	path    string
	options collection.Options
}

// NewSqliteStore opens a connection and ensures schemas are applied.
func NewSqliteStore(path string, opts collection.Options) (*SqliteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// Apply Pragmas
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma failed: %w", err)
		}
	}

	// Apply Schemas
	if _, err := db.Exec(collection.DefaultSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema failed: %w", err)
	}

	if opts.EnableJSON {
		// Check if column exists (simple check via error on duplicate column)
		db.Exec(collection.JSONSchema) 
	}

	if opts.EnableFTS {
		db.Exec(collection.FTSSchema)
	}
	
	if opts.EnableVector {
		schema := fmt.Sprintf(collection.VectorSchema, opts.VectorDimensions)
		db.Exec(schema)
	}

	return &SqliteStore{
		db:      db,
		path:    path,
		options: opts,
	}, nil
}

func (s *SqliteStore) Close() error {
	return s.db.Close()
}

func (s *SqliteStore) Path() string {
	return s.path
}

func (s *SqliteStore) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func (s *SqliteStore) ExecuteRaw(query string, args ...interface{}) error {
	_, err := s.db.Exec(query, args...)
	return err
}

// Helper to convert proto to JSON for indexing
func protoToJSON(p []byte) string {
    // Simplified: In a real implementation, you'd unmarshal to a dynamic message
    // or a generic map to get the JSON string. 
    // For now, we assume simple text conversion or pre-marshaled availability.
    return "{}" // Placeholder for dynamic unmarshal logic
}

func (s *SqliteStore) CreateRecord(ctx context.Context, r *pb.CollectionRecord) error {
	query := `INSERT INTO records (id, proto_data, data_uri, created_at, updated_at, labels) VALUES (?, ?, ?, ?, ?, ?)`
	labelsJSON, _ := json.Marshal(r.Metadata.Labels)
	args := []interface{}{
		r.Id, 
		r.ProtoData, 
		r.DataUri, 
		r.Metadata.CreatedAt.Seconds, 
		r.Metadata.UpdatedAt.Seconds,
		string(labelsJSON),
	}

	if s.options.EnableJSON {
		// In a full implementation, use dynamicpb to get proper JSON
		jsonTxt := protoToJSON(r.ProtoData)
		query = `INSERT INTO records (id, proto_data, data_uri, created_at, updated_at, labels, jsontext) VALUES (?, ?, ?, ?, ?, ?, ?)`
		args = append(args, jsonTxt)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		tx.Rollback()
		return err
	}

	// Update FTS if enabled (and using content='records' option)
	// If content='records' is used, we might need a trigger or manual rebuild. 
	// If independent FTS table, we insert here.
	if s.options.EnableFTS {
		// Manual sync example
		// _, err := tx.ExecContext(ctx, "INSERT INTO records_fts(id, content) VALUES (?, ?)", r.Id, flattenJSON(jsonTxt))
	}

	return tx.Commit()
}

func (s *SqliteStore) GetRecord(ctx context.Context, id string) (*pb.CollectionRecord, error) {
	var (
		protoData           []byte
		dataUri             sql.NullString
		createdAt, updatedAt int64
		labelsJSON          string
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT proto_data, data_uri, created_at, updated_at, labels
		FROM records WHERE id = ?
	`, id).Scan(&protoData, &dataUri, &createdAt, &updatedAt, &labelsJSON)

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
	if dataUri.Valid { record.DataUri = dataUri.String }
	if labelsJSON != "" {
		json.Unmarshal([]byte(labelsJSON), &record.Metadata.Labels)
	}
	return record, nil
}

func (s *SqliteStore) UpdateRecord(ctx context.Context, r *pb.CollectionRecord) error {
	// Simplified update
	query := `UPDATE records SET proto_data=?, updated_at=? WHERE id=?`
	args := []interface{}{r.ProtoData, r.Metadata.UpdatedAt.Seconds, r.Id}
	
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *SqliteStore) DeleteRecord(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM records WHERE id = ?", id)
	return err
}

func (s *SqliteStore) ListRecords(ctx context.Context, offset, limit int) ([]*pb.CollectionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, proto_data, data_uri, created_at, updated_at, labels
		FROM records ORDER BY created_at DESC LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*pb.CollectionRecord
	for rows.Next() {
		var r pb.CollectionRecord
		var created, updated int64
		var dUri sql.NullString
		var lJSON string
		
		rows.Scan(&r.Id, &r.ProtoData, &dUri, &created, &updated, &lJSON)
		r.Metadata = &pb.Metadata{
			CreatedAt: &pb.Timestamp{Seconds: created},
			UpdatedAt: &pb.Timestamp{Seconds: updated},
		}
		if dUri.Valid { r.DataUri = dUri.String }
		if lJSON != "" {
			json.Unmarshal([]byte(lJSON), &r.Metadata.Labels)
		}
		results = append(results, &r)
	}
	return results, nil
}

func (s *SqliteStore) CountRecords(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM records").Scan(&count)
	return count, err
}

func (s *SqliteStore) Search(ctx context.Context, q *collection.SearchQuery) ([]*collection.SearchResult, error) {
	// Note: This requires building a dynamic SQL query string based on filters.
	// This implementation mocks the structure.
	var results []*collection.SearchResult
	
	// Example FTS query construction
	if q.FullText != "" && s.options.EnableFTS {
		// "SELECT ... FROM records JOIN records_fts ON ... WHERE records_fts MATCH ?"
	}
	
	return results, nil
}
