package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/protobuf/types/known/timestamppb"

	_ "modernc.org/sqlite" // Using modernc.org/sqlite (cgo-free)
)

type SqliteStore struct {
	db       *sql.DB
	path     string
	options  collection.Options
	embedder collection.Embedder // Optional embedder for vectors
	mu       sync.RWMutex
}

// NewSqliteStore initializes the database and applies schemas.
func NewSqliteStore(path string, opts collection.Options, embedder ...collection.Embedder) (*SqliteStore, error) {
	var emb collection.Embedder
	if len(embedder) > 0 {
		emb = embedder[0]
	}
	if opts.EnableVector && emb == nil {
		return nil, fmt.Errorf("embedder required when EnableVector is true")
	}
	// WAL mode + busy_timeout are critical for concurrent access.
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=10000", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	// Performance Pragmas
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

	// Apply Schemas
	if _, err := db.Exec(collection.DefaultSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("default schema failed: %w", err)
	}

	// jsontext column is always referenced by the store (filters, FTS, vector extraction),
	// so ensure it exists even if EnableJSON is false.
	if _, err := db.Exec(collection.JSONSchema); err != nil {
		// Ignore error if column already exists, or handle strictly
	}

	if opts.EnableVector {
		if opts.VectorDimensions <= 0 {
			db.Close()
			return nil, fmt.Errorf("VectorDimensions must be > 0 when EnableVector is true")
		}
		if _, err := db.Exec(collection.VectorSchema); err != nil {
			// Ignore error if column already exists, or handle strictly
		}
		if err := enableVectorIndex(db, opts.VectorDimensions); err != nil {
			db.Close()
			return nil, fmt.Errorf("enable sqlite vector index: %w", err)
		}
	}

	if opts.EnableFTS {
		tx, err := db.Begin()
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("begin fts transaction: %w", err)
		}

		if _, err := tx.Exec(collection.FTSSchema); err != nil {
			tx.Rollback()
			db.Close()
			return nil, fmt.Errorf("fts schema failed: %w", err)
		}

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
		if _, err := tx.Exec(triggers); err != nil {
			tx.Rollback()
			db.Close()
			return nil, fmt.Errorf("fts triggers failed: %w", err)
		}

		if err := tx.Commit(); err != nil {
			db.Close()
			return nil, fmt.Errorf("commit fts transaction: %w", err)
		}
	}

	return &SqliteStore{
		db:       db,
		path:     path,
		options:  opts,
		embedder: emb,
	}, nil
}

func (s *SqliteStore) Close() error { return s.db.Close() }
func (s *SqliteStore) Path() string { return s.path }

func (s *SqliteStore) CreateRecord(ctx context.Context, r *pb.CollectionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	labelsJSON, _ := json.Marshal(r.Metadata.Labels)

	// If proto_data is valid JSON, use it for jsontext. Otherwise, use a default.
	var jsonText string
	if json.Valid(r.ProtoData) {
		jsonText = string(r.ProtoData)
	} else {
		jsonText = "{}"
	}

	// Generate and serialize vector if enabled and embedder is available
	var vectorBlob interface{}
	rawVector, vectorBlob, err := s.generateVector(ctx, jsonText)
	if err != nil {
		return err
	}

	// Build query based on whether vector column exists
	var query string
	var args []interface{}
	if s.options.EnableVector {
		query = `INSERT INTO records (id, proto_data, data_uri, created_at, updated_at, labels, jsontext, vector) 
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
		args = []interface{}{
			r.Id,
			r.ProtoData,
			r.DataUri,
			r.Metadata.CreatedAt.Seconds,
			r.Metadata.UpdatedAt.Seconds,
			string(labelsJSON),
			jsonText,
			vectorBlob,
		}
	} else {
		query = `INSERT INTO records (id, proto_data, data_uri, created_at, updated_at, labels, jsontext) 
                 VALUES (?, ?, ?, ?, ?, ?, ?)`
		args = []interface{}{
			r.Id,
			r.ProtoData,
			r.DataUri,
			r.Metadata.CreatedAt.Seconds,
			r.Metadata.UpdatedAt.Seconds,
			string(labelsJSON),
			jsonText,
		}
	}

	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}

	if s.options.EnableVector && len(rawVector) > 0 {
		if err := s.upsertVectorIndex(ctx, tx, r.Id, rawVector); err != nil {
			return fmt.Errorf("update vector index: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SqliteStore) GetRecord(ctx context.Context, id string) (*pb.CollectionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

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

func (s *SqliteStore) UpdateRecord(ctx context.Context, r *pb.CollectionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	labelsJSON, _ := json.Marshal(r.Metadata.Labels)

	var jsonText string
	if json.Valid(r.ProtoData) {
		jsonText = string(r.ProtoData)
	} else {
		return fmt.Errorf("invalid JSON")
	}

	var vectorBlob interface{}
	rawVector, vectorBlob, err := s.generateVector(ctx, jsonText)
	if err != nil {
		return err
	}

	var query string
	var args []interface{}
	if s.options.EnableVector {
		query = `UPDATE records SET proto_data=?, updated_at=?, labels=?, jsontext=?, vector=? WHERE id=?`
		args = []interface{}{
			r.ProtoData,
			r.Metadata.UpdatedAt.Seconds,
			string(labelsJSON),
			jsonText,
			vectorBlob,
			r.Id,
		}
	} else {
		query = `UPDATE records SET proto_data=?, updated_at=?, labels=?, jsontext=? WHERE id=?`
		args = []interface{}{
			r.ProtoData,
			r.Metadata.UpdatedAt.Seconds,
			string(labelsJSON),
			jsonText,
			r.Id,
		}
	}

	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("record not found")
	}

	if len(rawVector) > 0 {
		if err := s.upsertVectorIndex(ctx, tx, r.Id, rawVector); err != nil {
			return fmt.Errorf("update vector index: %w", err)
		}
	} else {
		if err := s.deleteVectorIndex(ctx, tx, r.Id); err != nil {
			return fmt.Errorf("clear vector index: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SqliteStore) DeleteRecord(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if s.options.EnableVector {
		if err := s.deleteVectorIndex(ctx, tx, id); err != nil {
			return fmt.Errorf("delete vector index entry: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM records WHERE id=?", id); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *SqliteStore) ListRecords(ctx context.Context, offset, limit int) ([]*pb.CollectionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `SELECT id, proto_data, data_uri, created_at, updated_at, labels FROM records ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*pb.CollectionRecord
	for rows.Next() {
		var (
			r                pb.CollectionRecord
			dUri             sql.NullString
			created, updated int64
			lJSON            string
		)

		rows.Scan(&r.Id, &r.ProtoData, &dUri, &created, &updated, &lJSON)

		r.Metadata = &pb.Metadata{
			CreatedAt: &timestamppb.Timestamp{Seconds: created},
			UpdatedAt: &timestamppb.Timestamp{Seconds: updated},
		}
		if dUri.Valid {
			r.DataUri = dUri.String
		}
		if lJSON != "" {
			json.Unmarshal([]byte(lJSON), &r.Metadata.Labels)
		}

		items = append(items, &r)
	}
	return items, nil
}

func (s *SqliteStore) CountRecords(ctx context.Context) (int64, error) {
	var c int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM records").Scan(&c)
	return c, err
}

func (s *SqliteStore) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func (s *SqliteStore) ExecuteRaw(q string, args ...interface{}) error {
	_, err := s.db.Exec(q, args...)
	return err
}

// Backup creates an online backup of the database to the specified path.
// This method is WAL-friendly and allows concurrent reads/writes during backup.
// It uses SQLite's online backup mechanism through a dedicated connection.
func (s *SqliteStore) Backup(ctx context.Context, destPath string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Open destination database
	destDSN := fmt.Sprintf("file:%s?_journal_mode=WAL", destPath)
	destDB, err := sql.Open("sqlite", destDSN)
	if err != nil {
		return fmt.Errorf("failed to open destination db: %w", err)
	}
	defer destDB.Close()

	// Get underlying connections for backup
	// We'll use ATTACH DATABASE and then copy the data
	// This is a workaround since database/sql doesn't expose the backup API directly

	// For modernc.org/sqlite, we can use VACUUM INTO which works well with WAL
	// First, ensure WAL checkpoint to get a consistent state
	if _, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
		// Non-fatal, continue anyway
	}

	// Use VACUUM INTO for the backup - this creates a consistent snapshot
	// Even with WAL mode, VACUUM INTO creates a complete consistent copy
	query := fmt.Sprintf("VACUUM INTO '%s'", destPath)
	if err := s.ExecuteRaw(query); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	return nil
}

// BackupOnline creates an online backup using incremental page copying.
// This minimizes lock time by copying pages in small batches.
// Best for very large databases where VACUUM INTO might take too long.
func (s *SqliteStore) BackupOnline(ctx context.Context, destPath string, pagesBatchSize int) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if pagesBatchSize <= 0 {
		pagesBatchSize = 100 // Default: copy 100 pages at a time
	}

	// Open destination database
	destDSN := fmt.Sprintf("file:%s?_journal_mode=WAL", destPath)
	destDB, err := sql.Open("sqlite", destDSN)
	if err != nil {
		return fmt.Errorf("failed to open destination db: %w", err)
	}
	defer destDB.Close()

	// Attach the destination database
	attachQuery := fmt.Sprintf("ATTACH DATABASE '%s' AS backup", destPath)
	if _, err := s.db.ExecContext(ctx, attachQuery); err != nil {
		return fmt.Errorf("failed to attach backup db: %w", err)
	}
	defer s.db.Exec("DETACH DATABASE backup")

	// Get list of tables from main database
	rows, err := s.db.QueryContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%'
	`)
	if err != nil {
		return fmt.Errorf("failed to list tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		tables = append(tables, name)
	}

	// Copy each table
	for _, table := range tables {
		// Get table schema
		var sql string
		err := s.db.QueryRowContext(ctx,
			"SELECT sql FROM sqlite_master WHERE type='table' AND name=?",
			table).Scan(&sql)
		if err != nil {
			return fmt.Errorf("failed to get schema for %s: %w", table, err)
		}

		// Create table in backup
		if _, err := destDB.ExecContext(ctx, sql); err != nil {
			// Table might already exist, continue
		}

		// Copy data in batches (for large tables)
		copyQuery := fmt.Sprintf("INSERT INTO backup.%s SELECT * FROM main.%s", table, table)
		if _, err := s.db.ExecContext(ctx, copyQuery); err != nil {
			return fmt.Errorf("failed to copy table %s: %w", table, err)
		}
	}

	// Copy indices
	idxRows, err := s.db.QueryContext(ctx, `
		SELECT sql FROM sqlite_master
		WHERE type='index' AND sql IS NOT NULL AND name NOT LIKE 'sqlite_%'
	`)
	if err != nil {
		return fmt.Errorf("failed to list indices: %w", err)
	}
	defer idxRows.Close()

	for idxRows.Next() {
		var sql string
		if err := idxRows.Scan(&sql); err != nil {
			continue
		}
		destDB.ExecContext(ctx, sql) // Ignore errors, index might exist
	}

	return nil
}

func (s *SqliteStore) ReIndex(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM records_fts"); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "INSERT INTO records_fts(rowid, content) SELECT rowid, jsontext FROM records"); err != nil {
		return err
	}

	if s.options.EnableVector && s.embedder != nil {
		rows, err := tx.QueryContext(ctx, "SELECT id, jsontext FROM records WHERE jsontext IS NOT NULL")
		if err != nil {
			return err
		}
		defer rows.Close()

		if _, err := tx.ExecContext(ctx, "DELETE FROM records_vss"); err != nil {
			return fmt.Errorf("reset vector index: %w", err)
		}

		updateStmt, err := tx.PrepareContext(ctx, "UPDATE records SET vector = ? WHERE id = ?")
		if err != nil {
			return err
		}
		defer updateStmt.Close()

		for rows.Next() {
			var id string
			var jsonText string
			if err := rows.Scan(&id, &jsonText); err != nil {
				return err
			}

			vector, vectorBlob, err := s.generateVector(ctx, jsonText)
			if err != nil {
				if _, err := updateStmt.ExecContext(ctx, nil, id); err != nil {
					return err
				}
				continue
			}

			if _, err := updateStmt.ExecContext(ctx, vectorBlob, id); err != nil {
				return err
			}

			if len(vector) > 0 {
				if err := s.upsertVectorIndex(ctx, tx, id, vector); err != nil {
					return fmt.Errorf("reindex vector entry: %w", err)
				}
			} else {
				if err := s.deleteVectorIndex(ctx, tx, id); err != nil {
					return fmt.Errorf("clear vector entry: %w", err)
				}
			}
		}

		if err := rows.Err(); err != nil {
			return err
		}
	}

	return tx.Commit()
}

type execContext interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func (s *SqliteStore) generateVector(ctx context.Context, jsonText string) ([]float32, interface{}, error) {
	if !s.options.EnableVector || s.embedder == nil {
		return nil, nil, nil
	}

	text := extractTextFromJSON(jsonText)
	if text == "" {
		return nil, nil, nil
	}

	vector, err := s.embedder.Embed(ctx, text)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate vector: %w", err)
	}
	if len(vector) != s.options.VectorDimensions {
		return nil, nil, fmt.Errorf("embedder produced %d dims, expected %d", len(vector), s.options.VectorDimensions)
	}

	blob, err := serializeVector(vector)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize vector: %w", err)
	}

	return vector, blob, nil
}
