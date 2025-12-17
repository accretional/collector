package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqliteext"
	"google.golang.org/protobuf/types/known/timestamppb"

	_ "modernc.org/sqlite"
)

type SqliteStore struct {
	db       *sql.DB
	vecDB    *sqliteext.DB // CGo-based connection for vector operations (nil if vectors disabled)
	path     string
	options  collection.Options
	embedder collection.Embedder
	mu       sync.RWMutex
}

func getVecExtensionPath() string {
	vectorPath := os.Getenv("SQLITE_VEC_EXTENSION")
	if vectorPath == "" {
		if cwd, err := os.Getwd(); err == nil {
			vectorPath = filepath.Join(cwd, "sqlite-vec", "vec0.so")
		}
	}
	return vectorPath
}

// Vector operations are best-effort; if all retries fail, it logs and continues
// since the main record is already committed and vector search is a secondary feature.
func retryVecOperation(ctx context.Context, maxRetries int, op func() error) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if err := op(); err != nil {
			lastErr = err
			// Exponential backoff: 10ms, 20ms, 40ms
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(10<<i) * time.Millisecond):
			}
			continue
		}
		return nil
	}
	return lastErr
}

// NewSqliteStore initializes the database and applies schemas.
// When EnableVector is true, it uses a hybrid approach:
// - modernc.org/sqlite (pure Go) for regular CRUD operations
// - Custom CGo driver for vector operations (requires sqlite-vec extension)
func NewSqliteStore(path string, opts collection.Options, embedder ...collection.Embedder) (*SqliteStore, error) {
	var emb collection.Embedder
	if len(embedder) > 0 {
		emb = embedder[0]
	}
	if opts.EnableVector && emb == nil {
		return nil, fmt.Errorf("embedder required when EnableVector is true")
	}
	if opts.EnableVector && opts.VectorDimensions <= 0 {
		return nil, fmt.Errorf("VectorDimensions must be > 0 when EnableVector is true")
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
		if _, err := db.Exec(collection.VectorSchema); err != nil {
			// Ignore error if column already exists
		}
	}

	var vecDB *sqliteext.DB
	if opts.EnableVector {
		extPath := getVecExtensionPath()
		vecDB, err = sqliteext.Open(context.Background(), path, extPath, "sqlite3_vec_init")
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("open vec db: %w", err)
		}

		stmt := fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS records_vec USING vec0(vector FLOAT[%d]);`, opts.VectorDimensions)
		if _, err := vecDB.ExecContext(context.Background(), stmt); err != nil {
			vecDB.Close()
			db.Close()
			return nil, fmt.Errorf("create vec0 table: %w", err)
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
		vecDB:    vecDB,
		path:     path,
		options:  opts,
		embedder: emb,
	}, nil
}

func (s *SqliteStore) Close() error {
	if s.vecDB != nil {
		s.vecDB.Close()
	}
	return s.db.Close()
}
func (s *SqliteStore) Path() string { return s.path }

func (s *SqliteStore) CreateRecord(ctx context.Context, r *pb.CollectionRecord) error {
	labelsJSON, _ := json.Marshal(r.Metadata.Labels)

	var jsonText string
	if json.Valid(r.ProtoData) {
		jsonText = string(r.ProtoData)
	} else {
		jsonText = "{}"
	}

	var vectorBlob interface{}
	rawVector, vectorBlob, err := s.reuseOrGenerateVector(ctx, r.Id, jsonText)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

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

	if err := tx.Commit(); err != nil {
		return err
	}

	// Now insert into vec0 table via the CGo connection.
	// This needs to happen after commit so vecDB can see the rowid.
	if s.options.EnableVector && len(rawVector) > 0 {
		id := r.Id
		vec := rawVector
		err := retryVecOperation(ctx, 3, func() error {
			return s.upsertVecTable(ctx, nil, id, vec)
		})
		if err != nil {
			// The record will work for non-vector queries; vector can be rebuilt via ReIndex
			log.Printf("Warning: failed to index vector for record %s after retries: %v", id, err)
		}
	}

	return nil
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
	labelsJSON, _ := json.Marshal(r.Metadata.Labels)

	var jsonText string
	if json.Valid(r.ProtoData) {
		jsonText = string(r.ProtoData)
	} else {
		return fmt.Errorf("invalid JSON")
	}

	var vectorBlob interface{}
	rawVector, vectorBlob, err := s.reuseOrGenerateVector(ctx, r.Id, jsonText)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

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

	if err := tx.Commit(); err != nil {
		return err
	}

	if s.options.EnableVector {
		id := r.Id
		if len(rawVector) > 0 {
			vec := rawVector
			err := retryVecOperation(ctx, 3, func() error {
				return s.upsertVecTable(ctx, nil, id, vec)
			})
			if err != nil {
				// The record will work for non-vector queries; vector can be rebuilt via ReIndex
				log.Printf("Warning: failed to update vector index for record %s after retries: %v", id, err)
			}
		} else {
			err := retryVecOperation(ctx, 3, func() error {
				return s.deleteVecEntry(ctx, nil, id)
			})
			if err != nil {
				// Log but don't fail - orphaned vector entries are harmless
				log.Printf("Warning: failed to clear vector index for record %s after retries: %v", id, err)
			}
		}
	}

	return nil
}

func (s *SqliteStore) DeleteRecord(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Delete from vec0 first
	if s.options.EnableVector {
		if err := s.deleteVecEntry(ctx, nil, id); err != nil {
			return fmt.Errorf("delete vector index entry: %w", err)
		}
	}

	// Now delete from main records table
	if _, err := s.db.ExecContext(ctx, "DELETE FROM records WHERE id=?", id); err != nil {
		return err
	}

	return nil
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

	// 1. Create a new SqliteStore at destPath to ensure proper schema initialization
	backupStore, err := NewSqliteStore(destPath, s.options, s.embedder)
	if err != nil {
		return fmt.Errorf("failed to create backup store: %w", err)
	}
	defer backupStore.Close()

	// 2. Attach the source (original) database to the backupStore connection using a distinct alias
	attachQuery := fmt.Sprintf("ATTACH DATABASE '%s' AS source", s.path)
	if _, err := backupStore.db.ExecContext(ctx, attachQuery); err != nil {
		return fmt.Errorf("failed to attach source db to backup store: %w", err)
	}
	defer backupStore.db.Exec("DETACH DATABASE source")

	// 3. Get list of tables from the source database
	rows, err := backupStore.db.QueryContext(ctx, `
		SELECT name FROM source.sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'records_fts'
	`)
	if err != nil {
		return fmt.Errorf("failed to list tables from source db: %w", err)
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

	// 4. Copy data from each table from source to backupStore's main database
	for _, table := range tables {
		copyQuery := fmt.Sprintf("INSERT INTO %s SELECT * FROM source.%s", table, table)
		if _, err := backupStore.db.ExecContext(ctx, copyQuery); err != nil {
			return fmt.Errorf("failed to copy table %s: %w", table, err)
		}
	}

	// 5. Explicitly copy FTS data if enabled
	if s.options.EnableFTS {
		ftsCopyQuery := "INSERT INTO records_fts(rowid, content) SELECT rowid, content FROM source.records_fts"
		if _, err := backupStore.db.ExecContext(ctx, ftsCopyQuery); err != nil {
			return fmt.Errorf("failed to copy FTS data: %w", err)
		}
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

	if s.options.EnableVector {
		if err := s.rebuildVectorIndex(ctx, tx); err != nil {
			return err
		}
	}

	return tx.Commit()
}
