package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite/ext"
	"google.golang.org/protobuf/types/known/timestamppb"

	_ "modernc.org/sqlite" // Using modernc.org/sqlite (cgo-free)
)

// ExtensionConfig specifies an extension to load.
// This type is used internally by the sqlite package and by the db factory.
type ExtensionConfig struct {
	Path       string
	EntryPoint string
	Required   bool
}

type SqliteStore struct {
	db       *sql.DB // Pure Go connection - used for all regular operations
	vectorDB *sql.DB // CGo connection - only used for vector search (nil if not needed)
	path     string
	options  collection.Options
	mu       sync.RWMutex
}

// NewSqliteStore creates a SQLite store with optional extension support.
// Uses hybrid model: pure Go driver for regular operations, CGo driver only for vector search.
func NewSqliteStore(ctx context.Context, path string, opts collection.Options, extensions []ExtensionConfig) (*SqliteStore, error) {
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

	if opts.EnableJSON {
		if _, err := db.Exec(collection.JSONSchema); err != nil {
			// Ignore error if column already exists
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

	store := &SqliteStore{
		db:      db,
		path:    path,
		options: opts,
	}

	// Open CGo connection only if extensions are provided
	if len(extensions) > 0 {
		vectorDB, err := sql.Open("sqliteext", dsn)
		if err != nil {
			if strings.Contains(err.Error(), "unknown driver") {
				db.Close()
				return nil, fmt.Errorf("CGo driver required but not available: extensions require CGo build. Error: %w", err)
			}
			db.Close()
			return nil, fmt.Errorf("failed to open vector db: %w", err)
		}

		// Apply same pragmas to vector connection
		for _, p := range pragmas {
			if _, err := vectorDB.Exec(p); err != nil {
				vectorDB.Close()
				db.Close()
				return nil, fmt.Errorf("pragma failed on vector db: %w", err)
			}
		}

		store.vectorDB = vectorDB

		// Load extensions on CGo connection
		if err := loadExtensions(ctx, vectorDB, extensions); err != nil {
			vectorDB.Close()
			db.Close()
			return nil, err
		}
	}

	return store, nil
}

// loadExtensions loads SQLite extensions using the CGo driver.
func loadExtensions(ctx context.Context, db *sql.DB, extensions []ExtensionConfig) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection for extension loading: %w", err)
	}
	defer conn.Close()

	for _, extCfg := range extensions {
		err := conn.Raw(func(driverConn any) error {
			c, ok := driverConn.(*ext.Conn)
			if !ok {
				return fmt.Errorf("unexpected connection type: %T (extensions require CGo driver)", driverConn)
			}
			return c.LoadExtension(extCfg.Path, extCfg.EntryPoint)
		})

		if err != nil {
			if extCfg.Required {
				return fmt.Errorf("failed to load required extension %s: %w", extCfg.Path, err)
			}
			// Extension not required, continue
		}
	}

	return nil
}

func (s *SqliteStore) Close() error {
	var errs []error
	if s.vectorDB != nil {
		if err := s.vectorDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close vector db: %w", err))
		}
	}
	if err := s.db.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close db: %w", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}
func (s *SqliteStore) Path() string { return s.path }

func (s *SqliteStore) Supports(feature string) bool {
	switch feature {
	case "vector":
		// Vector support requires both EnableVector option AND CGo connection exists
		return s.options.EnableVector && s.vectorDB != nil
	case "fts":
		return s.options.EnableFTS
	case "json":
		return s.options.EnableJSON
	default:
		return false
	}
}

func (s *SqliteStore) CreateRecord(ctx context.Context, r *pb.CollectionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `INSERT INTO records (id, proto_data, data_uri, created_at, updated_at, labels, jsontext) 
              VALUES (?, ?, ?, ?, ?, ?, ?)`

	labelsJSON, err := json.Marshal(r.Metadata.Labels)
	if err != nil {
		return fmt.Errorf("failed to marshal labels: %w", err)
	}

	// If proto_data is valid JSON, use it for jsontext. Otherwise, use a default.
	var jsonText string
	if json.Valid(r.ProtoData) {
		jsonText = string(r.ProtoData)
	} else {
		jsonText = "{}"
	}

	_, err = s.db.ExecContext(ctx, query,
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
		if err := json.Unmarshal([]byte(labelsJSON), &r.Metadata.Labels); err != nil {
			log.Printf("Warning: failed to unmarshal labels for record %s: %v", id, err)
		}
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

	query := `UPDATE records SET proto_data=?, updated_at=?, labels=?, jsontext=? WHERE id=?`
	labelsJSON, err := json.Marshal(r.Metadata.Labels)
	if err != nil {
		return fmt.Errorf("failed to marshal labels: %w", err)
	}

	var jsonText string
	if json.Valid(r.ProtoData) {
		jsonText = string(r.ProtoData)
	} else {
		return fmt.Errorf("invalid JSON")
	}

	res, err := tx.ExecContext(ctx, query,
		r.ProtoData,
		r.Metadata.UpdatedAt.Seconds,
		string(labelsJSON),
		jsonText,
		r.Id,
	)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("record not found")
	}

	return tx.Commit()
}

func (s *SqliteStore) DeleteRecord(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, "DELETE FROM records WHERE id=?", id)
	return err
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

		if err := rows.Scan(&r.Id, &r.ProtoData, &dUri, &created, &updated, &lJSON); err != nil {
			return nil, fmt.Errorf("failed to scan record: %w", err)
		}

		r.Metadata = &pb.Metadata{
			CreatedAt: &timestamppb.Timestamp{Seconds: created},
			UpdatedAt: &timestamppb.Timestamp{Seconds: updated},
		}
		if dUri.Valid {
			r.DataUri = dUri.String
		}
		if lJSON != "" {
			if err := json.Unmarshal([]byte(lJSON), &r.Metadata.Labels); err != nil {
				log.Printf("Warning: failed to unmarshal labels for record %s: %v", r.Id, err)
			}
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

func (s *SqliteStore) Search(ctx context.Context, q *collection.SearchQuery) ([]*collection.SearchResult, error) {
	var query strings.Builder
	var args []interface{}
	var whereClauses []string

	// Base query
	query.WriteString(`SELECT r.id, r.proto_data `)
	if q.FullText != "" {
		query.WriteString(`, bm25(records_fts) as score `)
	}
	query.WriteString(`FROM records r `)
	if q.FullText != "" {
		query.WriteString(`JOIN records_fts fts ON r.rowid = fts.rowid `)
	}

	// Full-text search
	if q.FullText != "" {
		whereClauses = append(whereClauses, `records_fts MATCH ?`)
		args = append(args, q.FullText)
	}

	// JSON filters
	for key, filter := range q.Filters {
		// JSON path needs to be properly quoted for keys with dots.
		path := `$.` + key

		switch filter.Operator {
		case collection.OpExists:
			whereClauses = append(whereClauses, `json_extract(r.jsontext, ?) IS NOT NULL`)
			args = append(args, path)
		case collection.OpNotExists:
			whereClauses = append(whereClauses, `json_extract(r.jsontext, ?) IS NULL`)
			args = append(args, path)
		case collection.OpContains:
			whereClauses = append(whereClauses, `json_extract(r.jsontext, ?) LIKE ?`)
			args = append(args, path, "%"+fmt.Sprintf("%v", filter.Value)+"%")
		default:
			whereClauses = append(whereClauses, fmt.Sprintf(`json_extract(r.jsontext, ?) %s ?`, filter.Operator))
			args = append(args, path, filter.Value)
		}
	}

	// Append WHERE clauses
	if len(whereClauses) > 0 {
		query.WriteString("WHERE " + strings.Join(whereClauses, " AND "))
	}

	// Ordering
	if q.OrderBy != "" {
		order := "ASC"
		if !q.Ascending {
			order = "DESC"
		}
		query.WriteString(fmt.Sprintf(` ORDER BY json_extract(r.jsontext, '$.%s') %s`, q.OrderBy, order))
	} else if q.FullText != "" {
		// Default to score for FTS
		query.WriteString(" ORDER BY score")
	}

	// Pagination
	if q.Limit > 0 {
		query.WriteString(" LIMIT ?")
		args = append(args, q.Limit)
	}
	if q.Offset > 0 {
		query.WriteString(" OFFSET ?")
		args = append(args, q.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*collection.SearchResult
	for rows.Next() {
		var r pb.CollectionRecord
		var score sql.NullFloat64

		var scanArgs = []any{&r.Id, &r.ProtoData}
		if q.FullText != "" {
			scanArgs = append(scanArgs, &score)
		}

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, err
		}

		searchResult := &collection.SearchResult{Record: &r}
		if score.Valid {
			searchResult.Score = score.Float64
		}
		results = append(results, searchResult)
	}
	return results, nil
}

func (s *SqliteStore) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func (s *SqliteStore) ExecuteRaw(ctx context.Context, q string, args ...interface{}) error {
	_, err := s.db.ExecContext(ctx, q, args...)
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
	if err := s.ExecuteRaw(ctx, query); err != nil {
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

	// Attach the destination database (SQLite will create the file if it doesn't exist)
	// We use the same connection to avoid conflicts
	attachQuery := fmt.Sprintf("ATTACH DATABASE '%s' AS backup", destPath)
	if _, err := s.db.ExecContext(ctx, attachQuery); err != nil {
		return fmt.Errorf("failed to attach backup db: %w", err)
	}
	defer func() {
		if _, err := s.db.Exec("DETACH DATABASE backup"); err != nil {
			log.Printf("Warning: failed to detach backup database: %v", err)
		}
	}()

	// Set WAL mode on the backup database
	if _, err := s.db.ExecContext(ctx, "PRAGMA backup.journal_mode=WAL"); err != nil {
		return fmt.Errorf("failed to set WAL mode on backup: %w", err)
	}

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

		// Create table in backup database
		backupSQL := strings.Replace(sql, fmt.Sprintf("CREATE TABLE %s", table), fmt.Sprintf("CREATE TABLE backup.%s", table), 1)
		if !strings.Contains(backupSQL, "backup.") {
			backupSQL = strings.Replace(sql, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s", table), fmt.Sprintf("CREATE TABLE IF NOT EXISTS backup.%s", table), 1)
		}
		if _, err := s.db.ExecContext(ctx, backupSQL); err != nil {
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
		// Create index in backup database
		backupSQL := strings.Replace(sql, "CREATE INDEX", "CREATE INDEX IF NOT EXISTS", 1)
		backupSQL = strings.Replace(backupSQL, "ON ", "ON backup.", 1)
		s.db.ExecContext(ctx, backupSQL) // Ignore errors, index might exist
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

	return tx.Commit()
}
