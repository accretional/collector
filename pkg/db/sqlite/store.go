package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/accretional/collector/pkg/collection"
	pb "github.com/accretional/collector/gen/collector"
	_ "modernc.org/sqlite" // Or generic driver
)

type SqliteStore struct {
	db      *sql.DB
	path    string
	options collection.Options
}

func NewSqliteStore(path string, opts collection.Options) (*SqliteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// Apply Pragma for performance/reliability
	db.Exec("PRAGMA journal_mode = WAL")
	db.Exec("PRAGMA synchronous = NORMAL")
	db.Exec("PRAGMA foreign_keys = ON")

	// Apply Schemas defined in pkg/collection
	if _, err := db.Exec(collection.DefaultSchema); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	if opts.EnableJSON {
		// Check if column exists or blindly try to add/ignore error
		db.Exec(collection.JSONSchema) 
	}

	if opts.EnableFTS {
		db.Exec(collection.FTSSchema)
	}
	
	if opts.EnableVector {
		// Note: Requires sqlite-vec extension loaded
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

func (s *SqliteStore) CreateRecord(ctx context.Context, r *pb.CollectionRecord) error {
	// Basic Insert
	query := `INSERT INTO records (id, proto_data, created_at, updated_at) VALUES (?, ?, ?, ?)`
	args := []interface{}{r.Id, r.ProtoData, r.Metadata.CreatedAt.Seconds, r.Metadata.UpdatedAt.Seconds}

	// Handle JSON
	if s.options.EnableJSON {
		// Assuming protoToJSON logic is available or injected
		jsonTxt := "{}" // Placeholder
		query = `INSERT INTO records (id, proto_data, created_at, updated_at, jsontext) VALUES (?, ?, ?, ?, ?)`
		args = append(args, jsonTxt)
	}

	// Transactional consistency
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		tx.Rollback()
		return err
	}

	// Handle Vector
	if s.options.EnableVector {
		// Extract vector from record (assuming it's in metadata or special field)
		// logic to insert into records_vec
	}

	return tx.Commit()
}

// ... GetRecord, UpdateRecord, etc impl ...

func (s *SqliteStore) Search(ctx context.Context, q *collection.SearchQuery) ([]*collection.SearchResult, error) {
	// Complex query builder logic here
	// Checks s.options to see if it can use FTS or JSON indexes
	// Checks q.Vector to see if it should use vec0
	return nil, nil
}

func (s *SqliteStore) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}
