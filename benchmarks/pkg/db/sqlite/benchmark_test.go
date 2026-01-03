//go:build benchmark
// +build benchmark

// Package sqlite provides benchmark tests for comparing SQLite driver implementations.
// This package is in benchmarks/ to keep benchmark code separate from production code.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/protobuf/types/known/timestamppb"
	_ "github.com/mattn/go-sqlite3"
)

// setupBenchmarkStore creates a store for benchmarking
func setupBenchmarkStore(tb testing.TB) (collection.Store, func()) {
	tempDir := tb.TempDir()
	dbPath := filepath.Join(tempDir, "bench.db")
	ctx := context.Background()

	store, err := setupCGoStore(ctx, dbPath)
	if err != nil {
		tb.Fatalf("Failed to setup store: %v", err)
	}

	// Apply schema
	if err := applySchema(store); err != nil {
		store.Close()
		tb.Fatalf("Failed to apply schema: %v", err)
	}

	return store, func() {
		store.Close()
	}
}

// setupCGoStore creates a store using mattn/go-sqlite3 (fully CGo)
// This function is only available when CGo is enabled (via build tag)
func setupCGoStore(ctx context.Context, dbPath string) (collection.Store, error) {
	// Check if sqlite3 driver is available (only when CGo is enabled)
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=10000", dbPath)

	// Try to open with sqlite3 driver (mattn/go-sqlite3)
	// If not available, this will fail gracefully
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		// If sqlite3 driver not available, try to provide helpful error
		if err.Error() == "sql: unknown driver \"sqlite3\" (forgotten import?)" {
			return nil, fmt.Errorf("sqlite3 driver not available: mattn/go-sqlite3 requires CGo. Install with: go get github.com/mattn/go-sqlite3")
		}
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	// Apply pragmas
	pragmas := []string{
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma failed: %w", err)
		}
	}

	// Create a wrapper that implements Store interface
	store := &cgoStore{
		db:   db,
		path: dbPath,
	}

	return store, nil
}

// applySchema applies the necessary schema to the store
func applySchema(store collection.Store) error {
	ctx := context.Background()

	// Apply default schema
	if err := store.ExecuteRaw(collection.DefaultSchema); err != nil {
		return fmt.Errorf("default schema failed: %w", err)
	}

	// Apply JSON schema (adds jsontext column)
	if err := store.ExecuteRaw(collection.JSONSchema); err != nil {
		// Ignore if column already exists
		errStr := err.Error()
		if !containsIgnoreCase(errStr, "duplicate column") && !containsIgnoreCase(errStr, "already exists") {
			return fmt.Errorf("JSON schema failed: %w", err)
		}
	}

	// Try to apply FTS schema - FTS5 may not be available in all SQLite builds
	ftsErr := store.ExecuteRaw(collection.FTSSchema)
	if ftsErr != nil {
		errStr := ftsErr.Error()
		// Check if it's just "already exists" error
		if containsIgnoreCase(errStr, "already exists") || containsIgnoreCase(errStr, "duplicate") {
			// Table already exists, that's fine - continue
		} else if containsIgnoreCase(errStr, "no such module") || containsIgnoreCase(errStr, "fts5") {
			// FTS5 not available - skip FTS features for this store
			// This is expected for some SQLite builds
			return nil
		} else {
			// Some other error - return it
			return fmt.Errorf("FTS schema failed: %w", ftsErr)
		}
	}

	// Verify FTS table exists before creating triggers
	// For cgoStore, check directly
	var ftsTableExists bool
	if cgoStore, ok := store.(*cgoStore); ok {
		var count int
		err := cgoStore.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='records_fts'").Scan(&count)
		if err == nil && count > 0 {
			ftsTableExists = true
		}
	} else {
		// For hybrid stores, if schema didn't error, assume table exists
		ftsTableExists = (ftsErr == nil)
	}

	// Apply FTS triggers only if FTS table exists
	if ftsTableExists {
		triggers := `
		CREATE TRIGGER IF NOT EXISTS records_ai AFTER INSERT ON records BEGIN
			INSERT INTO records_fts(rowid, content) VALUES (new.rowid, new.jsontext);
		END;
		CREATE TRIGGER IF NOT EXISTS records_ad AFTER DELETE ON records BEGIN
			DELETE FROM records_fts WHERE rowid=old.rowid;
		END;
		CREATE TRIGGER IF NOT EXISTS records_au AFTER UPDATE ON records BEGIN
			DELETE FROM records_fts WHERE rowid=old.rowid;
			INSERT INTO records_fts(rowid, content) VALUES (new.rowid, new.jsontext);
		END;
		`
		if err := store.ExecuteRaw(triggers); err != nil {
			errStr := err.Error()
			if !containsIgnoreCase(errStr, "already exists") && !containsIgnoreCase(errStr, "duplicate") {
				return fmt.Errorf("trigger creation failed: %w", err)
			}
		}
	}

	return nil
}

// containsIgnoreCase checks if a string contains a substring (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	s = strings.ToLower(s)
	substr = strings.ToLower(substr)
	return strings.Contains(s, substr)
}

// cgoStore is a wrapper around mattn/go-sqlite3 to implement Store interface
type cgoStore struct {
	db   *sql.DB
	path string
}

func (s *cgoStore) Close() error { return s.db.Close() }
func (s *cgoStore) Path() string { return s.path }

func (s *cgoStore) Supports(feature string) bool {
	// For benchmark purposes, assume all features supported
	return true
}

func (s *cgoStore) CreateRecord(ctx context.Context, r *pb.CollectionRecord) error {
	query := `INSERT INTO records (id, proto_data, data_uri, created_at, updated_at, labels, jsontext) 
              VALUES (?, ?, ?, ?, ?, ?, ?)`

	labelsJSON, _ := json.Marshal(r.Metadata.Labels)
	jsonText := "{}"
	if json.Valid(r.ProtoData) {
		jsonText = string(r.ProtoData)
	}

	_, err := s.db.ExecContext(ctx, query,
		r.Id,
		r.ProtoData,
		r.DataUri,
		r.Metadata.CreatedAt.Seconds,
		r.Metadata.UpdatedAt.Seconds,
		string(labelsJSON),
		jsonText,
	)
	return err
}

func (s *cgoStore) GetRecord(ctx context.Context, id string) (*pb.CollectionRecord, error) {
	var (
		protoData            []byte
		dataUri              sql.NullString
		createdAt, updatedAt int64
		labelsJSON           string
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT proto_data, data_uri, created_at, updated_at, labels
		FROM records WHERE id = ?`, id).Scan(&protoData, &dataUri, &createdAt, &updatedAt, &labelsJSON)

	if err != nil {
		return nil, err
	}

	r := &pb.CollectionRecord{
		Id:        id,
		ProtoData: protoData,
		Metadata: &pb.Metadata{
			CreatedAt: &timestamppb.Timestamp{Seconds: createdAt},
			UpdatedAt: &timestamppb.Timestamp{Seconds: updatedAt},
		},
	}
	if dataUri.Valid {
		r.DataUri = dataUri.String
	}
	if labelsJSON != "" {
		json.Unmarshal([]byte(labelsJSON), &r.Metadata.Labels)
	}

	return r, nil
}

func (s *cgoStore) UpdateRecord(ctx context.Context, r *pb.CollectionRecord) error {
	labelsJSON, _ := json.Marshal(r.Metadata.Labels)
	jsonText := "{}"
	if json.Valid(r.ProtoData) {
		jsonText = string(r.ProtoData)
	}

	query := `UPDATE records SET proto_data=?, updated_at=?, labels=?, jsontext=? WHERE id=?`
	res, err := s.db.ExecContext(context.Background(), query,
		r.ProtoData,
		r.Metadata.UpdatedAt.Seconds,
		string(labelsJSON),
		jsonText,
		r.Id,
	)
	if err != nil {
		return err
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("record not found")
	}
	return nil
}

func (s *cgoStore) DeleteRecord(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM records WHERE id = ?", id)
	return err
}

func (s *cgoStore) ListRecords(ctx context.Context, offset, limit int) ([]*pb.CollectionRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, proto_data, data_uri, created_at, updated_at, labels 
		 FROM records ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*pb.CollectionRecord
	for rows.Next() {
		var r pb.CollectionRecord
		var dataUri sql.NullString
		var createdAt, updatedAt int64
		var labelsJSON string

		if err := rows.Scan(&r.Id, &r.ProtoData, &dataUri, &createdAt, &updatedAt, &labelsJSON); err != nil {
			return nil, err
		}

		r.Metadata = &pb.Metadata{
			CreatedAt: &timestamppb.Timestamp{Seconds: createdAt},
			UpdatedAt: &timestamppb.Timestamp{Seconds: updatedAt},
		}
		if dataUri.Valid {
			r.DataUri = dataUri.String
		}
		if labelsJSON != "" {
			json.Unmarshal([]byte(labelsJSON), &r.Metadata.Labels)
		}

		items = append(items, &r)
	}
	return items, nil
}

func (s *cgoStore) CountRecords(ctx context.Context) (int64, error) {
	var c int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM records").Scan(&c)
	return c, err
}

func (s *cgoStore) Search(ctx context.Context, q *collection.SearchQuery) ([]*collection.SearchResult, error) {
	// Implement basic search for CGo store
	// This is a simplified version for benchmarking
	var query string
	var args []interface{}

	if q.FullText != "" {
		query = `SELECT r.id, r.proto_data, bm25(records_fts) as score 
		         FROM records r 
		         JOIN records_fts fts ON r.rowid = fts.rowid 
		         WHERE records_fts MATCH ? 
		         ORDER BY score LIMIT ?`
		args = []interface{}{q.FullText, q.Limit}
	} else {
		query = `SELECT r.id, r.proto_data 
		         FROM records r 
		         LIMIT ?`
		args = []interface{}{q.Limit}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*collection.SearchResult
	for rows.Next() {
		var r pb.CollectionRecord
		var score sql.NullFloat64

		scanArgs := []interface{}{&r.Id, &r.ProtoData}
		if q.FullText != "" {
			scanArgs = append(scanArgs, &score)
		}

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, err
		}

		result := &collection.SearchResult{Record: &r}
		if score.Valid {
			result.Score = score.Float64
		}
		results = append(results, result)
	}

	return results, nil
}

func (s *cgoStore) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func (s *cgoStore) ReIndex(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "REINDEX")
	return err
}

func (s *cgoStore) Backup(ctx context.Context, destPath string) error {
	query := fmt.Sprintf("VACUUM INTO '%s'", destPath)
	_, err := s.db.ExecContext(ctx, query)
	return err
}

func (s *cgoStore) ExecuteRaw(query string, args ...interface{}) error {
	_, err := s.db.ExecContext(context.Background(), query, args...) // Using context.Background() as interface doesn't provide context
	return err
}

// Benchmark CRUD Operations

func BenchmarkCreateRecord(b *testing.B) {
	store, cleanup := setupBenchmarkStore(b)
	defer cleanup()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("bench-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"id": %d, "name": "Record %d"}`, i, i)),
			Metadata: &pb.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
		}
		if err := store.CreateRecord(ctx, record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetRecord(b *testing.B) {
	store, cleanup := setupBenchmarkStore(b)
	defer cleanup()
	ctx := context.Background()

	// Setup: create 1000 records
	for i := 0; i < 1000; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("bench-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
			Metadata: &pb.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
		}
		store.CreateRecord(ctx, record)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-%d", i%1000)
		if _, err := store.GetRecord(ctx, id); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUpdateRecord(b *testing.B) {
	store, cleanup := setupBenchmarkStore(b)
	defer cleanup()
	ctx := context.Background()

	// Setup
	record := &pb.CollectionRecord{
		Id:        "bench-update",
		ProtoData: []byte(`{"version": 0}`),
		Metadata: &pb.Metadata{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
		},
	}
	store.CreateRecord(ctx, record)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		record.ProtoData = []byte(fmt.Sprintf(`{"version": %d}`, i))
		record.Metadata.UpdatedAt = timestamppb.Now()
		if err := store.UpdateRecord(ctx, record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDeleteRecord(b *testing.B) {
	store, cleanup := setupBenchmarkStore(b)
	defer cleanup()
	ctx := context.Background()

	// Setup: create records for deletion
	ids := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("delete-%d", i)
		ids[i] = id
		record := &pb.CollectionRecord{
			Id:        id,
			ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
			Metadata: &pb.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
		}
		store.CreateRecord(ctx, record)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.DeleteRecord(ctx, ids[i]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListRecords(b *testing.B) {
	store, cleanup := setupBenchmarkStore(b)
	defer cleanup()
	ctx := context.Background()

	// Setup: create 1000 records
	for i := 0; i < 1000; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("list-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
			Metadata: &pb.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
		}
		store.CreateRecord(ctx, record)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.ListRecords(ctx, 0, 100); err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark Search Operations

func BenchmarkSearch_FullText(b *testing.B) {
	store, cleanup := setupBenchmarkStore(b)
	defer cleanup()
	ctx := context.Background()

	// Setup: create searchable records
	for i := 0; i < 1000; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("search-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"content": "searchable content number %d with various terms and keywords"}`, i)),
			Metadata: &pb.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
		}
		store.CreateRecord(ctx, record)
	}

	// Give FTS index time to build
	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Search(ctx, &collection.SearchQuery{
			FullText: "searchable content",
			Limit:    10,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearch_JSONBFilter(b *testing.B) {
	store, cleanup := setupBenchmarkStore(b)
	defer cleanup()
	ctx := context.Background()

	// Setup: create records with JSON fields
	for i := 0; i < 1000; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("filter-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"score": %d, "status": "active", "category": "test"}`, i)),
			Metadata: &pb.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
		}
		store.CreateRecord(ctx, record)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Search(ctx, &collection.SearchQuery{
			Filters: map[string]collection.Filter{
				"status":   {Operator: collection.OpEquals, Value: "active"},
				"score":    {Operator: collection.OpGreaterThan, Value: 500},
				"category": {Operator: collection.OpEquals, Value: "test"},
			},
			Limit: 10,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearch_Combined(b *testing.B) {
	store, cleanup := setupBenchmarkStore(b)
	defer cleanup()
	ctx := context.Background()

	// Setup: create records with both text and JSON
	for i := 0; i < 1000; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("combined-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"title": "Document %d", "score": %d, "status": "active"}`, i, i)),
			Metadata: &pb.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
		}
		store.CreateRecord(ctx, record)
	}

	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Search(ctx, &collection.SearchQuery{
			FullText: "Document",
			Filters: map[string]collection.Filter{
				"status": {Operator: collection.OpEquals, Value: "active"},
				"score":  {Operator: collection.OpGreaterThan, Value: 500},
			},
			Limit: 10,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark Concurrent Operations

func BenchmarkConcurrentReads(b *testing.B) {
	store, cleanup := setupBenchmarkStore(b)
	defer cleanup()
	ctx := context.Background()

	// Setup: create 100 records
	for i := 0; i < 100; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("concurrent-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
			Metadata: &pb.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
		}
		store.CreateRecord(ctx, record)
	}

	b.ResetTimer()
	b.RunParallel(func(p *testing.PB) {
		i := 0
		for p.Next() {
			id := fmt.Sprintf("concurrent-%d", i%100)
			if _, err := store.GetRecord(ctx, id); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

func BenchmarkConcurrentWrites(b *testing.B) {
	store, cleanup := setupBenchmarkStore(b)
	defer cleanup()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(p *testing.PB) {
		i := 0
		for p.Next() {
			record := &pb.CollectionRecord{
				Id:        fmt.Sprintf("write-%d-%d", time.Now().UnixNano(), i),
				ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
				Metadata: &pb.Metadata{
					CreatedAt: timestamppb.Now(),
					UpdatedAt: timestamppb.Now(),
				},
			}
			if err := store.CreateRecord(ctx, record); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}
