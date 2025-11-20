package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/accretional/collector/pkg/collection"
	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/protobuf/types/known/timestamppb"

	_ "modernc.org/sqlite" // Using modernc.org/sqlite (cgo-free)
)

type SqliteStore struct {
	db      *sql.DB
	path    string
	options collection.Options
	mu      sync.RWMutex
}

// NewSqliteStore initializes the database and applies schemas.
func NewSqliteStore(path string, opts collection.Options) (*SqliteStore, error) {
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

	if opts.EnableJSON {
		if _, err := db.Exec(collection.JSONSchema); err != nil {
			// Ignore error if column already exists, or handle strictly
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

	return &SqliteStore{db: db, path: path, options: opts}, nil
}

func (s *SqliteStore) Close() error { return s.db.Close() }
func (s *SqliteStore) Path() string { return s.path }

func (s *SqliteStore) CreateRecord(ctx context.Context, r *pb.CollectionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `INSERT INTO records (id, proto_data, data_uri, created_at, updated_at, labels, jsontext) 
              VALUES (?, ?, ?, ?, ?, ?, ?)`
	
	labelsJSON, _ := json.Marshal(r.Metadata.Labels)

	// If proto_data is valid JSON, use it for jsontext. Otherwise, use a default.
	var jsonText string
	if json.Valid(r.ProtoData) {
		jsonText = string(r.ProtoData)
	} else {
		jsonText = "{}"
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
	if dataUri.Valid { r.DataUri = dataUri.String }
	if labelsJSON != "" { json.Unmarshal([]byte(labelsJSON), &r.Metadata.Labels) }
	
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
	labelsJSON, _ := json.Marshal(r.Metadata.Labels)
	
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

	rows, _ := res.RowsAffected()
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
			r pb.CollectionRecord
			dUri sql.NullString
			created, updated int64
			lJSON string
		)
		
		rows.Scan(&r.Id, &r.ProtoData, &dUri, &created, &updated, &lJSON)
		
		r.Metadata = &pb.Metadata{
			CreatedAt: &timestamppb.Timestamp{Seconds: created},
			UpdatedAt: &timestamppb.Timestamp{Seconds: updated},
		}
		if dUri.Valid { r.DataUri = dUri.String }
		if lJSON != "" { json.Unmarshal([]byte(lJSON), &r.Metadata.Labels) }

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

func (s *SqliteStore) ExecuteRaw(q string, args ...interface{}) error {
	_, err := s.db.Exec(q, args...)
	return err
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
