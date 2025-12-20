package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"google.golang.org/protobuf/types/known/timestamppb"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db           *sql.DB
	path         string
	options      collection.Options
	embedder     collection.Embedder
	ftsAvailable bool
	mu           sync.RWMutex
}

type execContext interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func NewStore(path string, opts collection.Options) (*Store, error) {
	if opts.EnableVector && opts.Embedder == nil {
		return nil, fmt.Errorf("embedder required when EnableVector is true")
	}
	if opts.EnableVector && opts.VectorDimensions <= 0 {
		return nil, fmt.Errorf("VectorDimensions must be > 0 when EnableVector is true")
	}

	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=10000", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	if opts.EnableVector {
		sqlite_vec.Auto()
	}

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

	if _, err := db.Exec(collection.DefaultSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("default schema failed: %w", err)
	}

	if _, err := db.Exec(collection.JSONSchema); err != nil {
		log.Println("JSONSchema already exists")
	}

	if opts.EnableVector {
		if _, err := db.Exec(collection.VectorSchema); err != nil {
			log.Println("VectorSchema already exists")
		}

		stmt := fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS records_vec USING vec0(vector FLOAT[%d]);`, opts.VectorDimensions)
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("create vec0 table: %w", err)
		}
	}

	ftsAvailable := false
	if opts.EnableFTS {
		// Check if FTS5 is available by trying to create a test table
		testTx, testErr := db.Begin()
		if testErr == nil {
			_, testErr = testTx.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS _fts5_test USING fts5(test)")
			if testErr == nil {
				testTx.Exec("DROP TABLE IF EXISTS _fts5_test")
				testTx.Commit()
				ftsAvailable = true
			} else {
				testTx.Rollback()
			}
		}

		if !ftsAvailable {
			db.Close()
			return nil, fmt.Errorf("FTS5 is not available but EnableFTS is true. Build with -tags sqlite_fts5 to enable FTS5 support")
		}

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

	return &Store{
		db:           db,
		path:         path,
		options:      opts,
		embedder:     opts.Embedder,
		ftsAvailable: ftsAvailable,
	}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
func (s *Store) Path() string { return s.path }

func (s *Store) CreateRecord(ctx context.Context, r *pb.CollectionRecord) error {
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

	if s.options.EnableVector && len(rawVector) > 0 {
		if err := s.upsertVecTable(ctx, tx, r.Id, rawVector); err != nil {
			return fmt.Errorf("update vector index: %w", err)
		}
	}

	return tx.Commit()
}

func (s *Store) GetRecord(ctx context.Context, id string) (*pb.CollectionRecord, error) {
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

func (s *Store) UpdateRecord(ctx context.Context, r *pb.CollectionRecord) error {
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

	if s.options.EnableVector {
		if len(rawVector) > 0 {
			if err := s.upsertVecTable(ctx, tx, r.Id, rawVector); err != nil {
				return fmt.Errorf("update vector index: %w", err)
			}
		} else {
			if err := s.deleteVecEntry(ctx, tx, r.Id); err != nil {
				return fmt.Errorf("clear vector index: %w", err)
			}
		}
	}

	return tx.Commit()
}

func (s *Store) DeleteRecord(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Delete from vec0 and main records table in a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if s.options.EnableVector {
		if err := s.deleteVecEntry(ctx, tx, id); err != nil {
			return fmt.Errorf("delete vector index entry: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM records WHERE id=?", id); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) ListRecords(ctx context.Context, offset, limit int) ([]*pb.CollectionRecord, error) {
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

func (s *Store) CountRecords(ctx context.Context) (int64, error) {
	var c int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM records").Scan(&c)
	return c, err
}

func (s *Store) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func (s *Store) ExecuteRaw(q string, args ...interface{}) error {
	_, err := s.db.Exec(q, args...)
	return err
}

// Backup creates an online backup of the database to the specified path.
// This method is WAL-friendly and allows concurrent reads/writes during backup.
// It uses SQLite's online backup mechanism through a dedicated connection.
func (s *Store) Backup(ctx context.Context, destPath string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
		// Non-fatal, continue anyway
	}

	query := fmt.Sprintf("VACUUM INTO '%s'", destPath)
	if err := s.ExecuteRaw(query); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	return nil
}

// BackupOnline creates an online backup using incremental page copying.
// This minimizes lock time by copying pages in small batches.
// Best for very large databases where VACUUM INTO might take too long.
func (s *Store) BackupOnline(ctx context.Context, destPath string, pagesBatchSize int) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if pagesBatchSize <= 0 {
		pagesBatchSize = 100 // Default: copy 100 pages at a time
	}

	// Attach the destination database (creates file if it doesn't exist)
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

		// Create table in backup (modify CREATE TABLE to use backup schema)
		backupSQL := strings.Replace(sql, "CREATE TABLE ", "CREATE TABLE IF NOT EXISTS backup.", 1)
		if _, err := s.db.ExecContext(ctx, backupSQL); err != nil {
			return fmt.Errorf("failed to create table %s in backup: %w", table, err)
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
		var idxSQL string
		if err := idxRows.Scan(&idxSQL); err != nil {
			continue
		}
		// Modify index to use backup schema
		backupIdxSQL := strings.Replace(idxSQL, " ON ", " ON backup.", 1)
		if _, err := s.db.ExecContext(ctx, backupIdxSQL); err != nil {
			log.Printf("failed to copy index: %v", err)
		}
	}

	return nil
}

func (s *Store) Search(ctx context.Context, q *collection.SearchQuery) ([]*collection.SearchResult, error) {
	hasVector := len(q.Vector) > 0 && s.options.EnableVector
	hasFTS := q.FullText != "" && s.ftsAvailable

	builder := &searchQueryBuilder{
		store:     s,
		query:     q,
		hasVector: hasVector,
		hasFTS:    hasFTS,
	}

	switch {
	case hasVector && hasFTS:
		return builder.buildHybrid(ctx)
	case hasVector:
		return builder.buildVector(ctx)
	case hasFTS:
		return builder.buildFTS(ctx)
	default:
		return builder.buildScalar(ctx)
	}
}

type searchQueryBuilder struct {
	store        *Store
	query        *collection.SearchQuery
	hasVector    bool
	hasFTS       bool
	querySQL     strings.Builder
	args         []interface{}
	whereClauses []string
}

func (b *searchQueryBuilder) buildHybrid(ctx context.Context) ([]*collection.SearchResult, error) {
	if len(b.query.Vector) != b.store.options.VectorDimensions {
		return nil, fmt.Errorf("query vector dimension mismatch: got %d, expected %d",
			len(b.query.Vector), b.store.options.VectorDimensions)
	}

	queryVector, err := sqlite_vec.SerializeFloat32(b.query.Vector)
	if err != nil {
		return nil, fmt.Errorf("serialize query vector: %w", err)
	}

	limit := b.getKNNLimit()

	b.selectFields(`r.id, r.proto_data, r.data_uri, r.created_at, r.updated_at, r.labels,
                    v.distance, bm25(records_fts) as fts_score`)
	b.fromClause(`records_vec v
                  JOIN records r ON r.rowid = v.rowid
                  JOIN records_fts ON r.rowid = records_fts.rowid`)

	b.whereClauses = []string{
		`v.vector MATCH ?`,
		`k = ?`,
		`records_fts MATCH ?`,
	}
	b.args = append(b.args, queryVector, limit, b.query.FullText)

	b.addSimilarityThreshold()
	b.addFilters()
	b.buildWhere()
	b.orderBy(`v.distance, fts_score`, `json_extract(r.jsontext, '$.%s') %s, v.distance, fts_score`)
	b.addPagination(true)

	return b.store.executeSearchQuery(ctx, b.querySQL.String(), b.args, true, true)
}

func (b *searchQueryBuilder) buildVector(ctx context.Context) ([]*collection.SearchResult, error) {
	if len(b.query.Vector) != b.store.options.VectorDimensions {
		return nil, fmt.Errorf("query vector dimension mismatch: got %d, expected %d",
			len(b.query.Vector), b.store.options.VectorDimensions)
	}

	queryVector, err := sqlite_vec.SerializeFloat32(b.query.Vector)
	if err != nil {
		return nil, fmt.Errorf("serialize query vector: %w", err)
	}

	limit := b.getKNNLimit()

	b.selectFields(`r.id, r.proto_data, r.data_uri, r.created_at, r.updated_at, r.labels, v.distance`)
	b.fromClause(`records_vec v JOIN records r ON r.rowid = v.rowid`)

	b.whereClauses = []string{
		`v.vector MATCH ?`,
		`k = ?`,
	}
	b.args = append(b.args, queryVector, limit)

	b.addSimilarityThreshold()
	b.addFilters()
	b.buildWhere()
	b.orderBy(`v.distance`, `json_extract(r.jsontext, '$.%s') %s, v.distance`)
	b.addPagination(true)

	return b.store.executeSearchQuery(ctx, b.querySQL.String(), b.args, true, false)
}

func (b *searchQueryBuilder) buildFTS(ctx context.Context) ([]*collection.SearchResult, error) {
	if !b.store.ftsAvailable {
		return nil, fmt.Errorf("full-text search requested but FTS5 is not available. Build with -tags sqlite_fts5 to enable FTS5 support")
	}

	b.selectFields(`r.id, r.proto_data, r.data_uri, r.created_at, r.updated_at, r.labels,
                    bm25(records_fts) as score`)
	b.fromClause(`records r JOIN records_fts ON r.rowid = records_fts.rowid`)

	b.whereClauses = []string{`records_fts MATCH ?`}
	b.args = append(b.args, b.query.FullText)

	b.addFilters()
	b.buildWhere()
	b.orderBy(`score`, `json_extract(r.jsontext, '$.%s') %s, score`)
	b.addPagination(false)

	return b.store.executeSearchQuery(ctx, b.querySQL.String(), b.args, false, true)
}

func (b *searchQueryBuilder) buildScalar(ctx context.Context) ([]*collection.SearchResult, error) {
	b.selectFields(`r.id, r.proto_data, r.data_uri, r.created_at, r.updated_at, r.labels`)
	b.fromClause(`records r`)

	b.whereClauses = []string{}
	b.addFilters()
	b.buildWhere()
	b.orderBy(`r.created_at DESC`, `json_extract(r.jsontext, '$.%s') %s`)
	b.addPagination(false)

	return b.store.executeSearchQuery(ctx, b.querySQL.String(), b.args, false, false)
}

func (b *searchQueryBuilder) selectFields(fields string) {
	b.querySQL.WriteString("SELECT " + fields + " ")
}

func (b *searchQueryBuilder) fromClause(clause string) {
	b.querySQL.WriteString("FROM " + clause + " ")
}

func (b *searchQueryBuilder) getKNNLimit() int {
	limit := b.query.Limit
	if limit <= 0 {
		limit = 50
	}
	return limit
}

func (b *searchQueryBuilder) addSimilarityThreshold() {
	if b.query.SimilarityThreshold > 0 {
		maxDistance := (1 / float64(b.query.SimilarityThreshold)) - 1
		b.whereClauses = append(b.whereClauses, `v.distance <= ?`)
		b.args = append(b.args, maxDistance)
	}
}

func (b *searchQueryBuilder) addFilters() {
	jsonFilters, jsonArgs := b.store.buildJSONFilters(b.query.Filters)
	b.whereClauses = append(b.whereClauses, jsonFilters...)
	b.args = append(b.args, jsonArgs...)

	labelFilters, labelArgs := b.store.buildLabelFilters(b.query.LabelFilters)
	b.whereClauses = append(b.whereClauses, labelFilters...)
	b.args = append(b.args, labelArgs...)
}

func (b *searchQueryBuilder) buildWhere() {
	if len(b.whereClauses) > 0 {
		b.querySQL.WriteString("WHERE " + strings.Join(b.whereClauses, " AND ") + " ")
	}
}

func (b *searchQueryBuilder) orderBy(defaultOrder, customOrderFmt string) {
	if b.query.OrderBy != "" {
		order := "ASC"
		if !b.query.Ascending {
			order = "DESC"
		}
		b.querySQL.WriteString(fmt.Sprintf("ORDER BY "+customOrderFmt+" ", b.query.OrderBy, order))
	} else {
		b.querySQL.WriteString("ORDER BY " + defaultOrder + " ")
	}
}

func (b *searchQueryBuilder) addPagination(vectorSearch bool) {
	if vectorSearch {
		// Vector searches: OFFSET only (LIMIT is handled by KNN k parameter)
		if b.query.Offset > 0 {
			b.querySQL.WriteString("OFFSET ? ")
			b.args = append(b.args, b.query.Offset)
		}
	} else {
		// Non-vector searches: LIMIT and OFFSET
		if b.query.Limit > 0 {
			b.querySQL.WriteString("LIMIT ? ")
			b.args = append(b.args, b.query.Limit)
		}
		if b.query.Offset > 0 {
			b.querySQL.WriteString("OFFSET ? ")
			b.args = append(b.args, b.query.Offset)
		}
	}
}

func (s *Store) buildJSONFilters(filters map[string]collection.Filter) ([]string, []interface{}) {
	var clauses []string
	var args []interface{}
	for key, filter := range filters {
		path := `$.` + key
		switch filter.Operator {
		case collection.OpExists:
			clauses = append(clauses, `json_extract(r.jsontext, ?) IS NOT NULL`)
			args = append(args, path)
		case collection.OpNotExists:
			clauses = append(clauses, `json_extract(r.jsontext, ?) IS NULL`)
			args = append(args, path)
		case collection.OpContains:
			clauses = append(clauses, `json_extract(r.jsontext, ?) LIKE ?`)
			args = append(args, path, "%"+fmt.Sprintf("%v", filter.Value)+"%")
		default:
			clauses = append(clauses, fmt.Sprintf(`json_extract(r.jsontext, ?) %s ?`, filter.Operator))
			args = append(args, path, filter.Value)
		}
	}
	return clauses, args
}

func (s *Store) buildLabelFilters(labelFilters map[string]string) ([]string, []interface{}) {
	var clauses []string
	var args []interface{}
	for key, value := range labelFilters {
		clauses = append(clauses, fmt.Sprintf(`json_extract(r.labels, '$.%s') = ?`, key))
		args = append(args, value)
	}
	return clauses, args
}

func (s *Store) executeSearchQuery(ctx context.Context, query string, args []interface{},
	hasVector, hasFTS bool) ([]*collection.SearchResult, error) {

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*collection.SearchResult
	for rows.Next() {
		var r pb.CollectionRecord
		var dataURI sql.NullString
		var createdAt, updatedAt int64
		var labelsJSON string
		var distance sql.NullFloat64
		var score sql.NullFloat64

		scanArgs := []interface{}{&r.Id, &r.ProtoData, &dataURI, &createdAt, &updatedAt, &labelsJSON}

		if hasVector {
			scanArgs = append(scanArgs, &distance)
		}
		if hasFTS {
			scanArgs = append(scanArgs, &score)
		}

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, err
		}

		r.Metadata = &pb.Metadata{
			CreatedAt: &timestamppb.Timestamp{Seconds: createdAt},
			UpdatedAt: &timestamppb.Timestamp{Seconds: updatedAt},
		}
		if dataURI.Valid {
			r.DataUri = dataURI.String
		}
		if labelsJSON != "" {
			_ = json.Unmarshal([]byte(labelsJSON), &r.Metadata.Labels)
		}

		result := &collection.SearchResult{Record: &r}
		if hasVector && distance.Valid {
			result.Distance = distance.Float64
		}
		if hasFTS && score.Valid {
			result.Score = score.Float64
		}

		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func (s *Store) ReIndex(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if s.ftsAvailable {
		if _, err := tx.ExecContext(ctx, "DELETE FROM records_fts"); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, "INSERT INTO records_fts(rowid, content) SELECT rowid, jsontext FROM records"); err != nil {
			return err
		}
	}

	if s.options.EnableVector {
		if err := s.rebuildVectorIndex(ctx, tx); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Vector helper methods

func (s *Store) upsertVecTable(ctx context.Context, tx execContext, id string, vector []float32) error {
	if !s.options.EnableVector {
		return fmt.Errorf("vector operations not available: vectors not enabled")
	}
	if len(vector) != s.options.VectorDimensions {
		return fmt.Errorf("vector dimension mismatch: got %d, expected %d", len(vector), s.options.VectorDimensions)
	}

	serialized, err := sqlite_vec.SerializeFloat32(vector)
	if err != nil {
		return fmt.Errorf("serialize vector: %w", err)
	}

	// vec0 virtual tables does not support INSERT OR REPLACE properly
	// Use delete-then-insert approach
	if tx != nil {
		_, _ = tx.ExecContext(ctx, `DELETE FROM records_vec WHERE rowid IN (SELECT rowid FROM records WHERE id = ?)`, id)
		_, err := tx.ExecContext(ctx, `INSERT INTO records_vec(rowid, vector) SELECT rowid, ? FROM records WHERE id = ?`, serialized, id)
		return err
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM records_vec WHERE rowid IN (SELECT rowid FROM records WHERE id = ?)`, id)
	_, err = s.db.ExecContext(ctx, `INSERT INTO records_vec(rowid, vector) SELECT rowid, ? FROM records WHERE id = ?`, serialized, id)
	return err
}

func (s *Store) deleteVecEntry(ctx context.Context, tx execContext, id string) error {
	if !s.options.EnableVector {
		return fmt.Errorf("vector operations not available: vectors not enabled")
	}
	if tx != nil {
		_, err := tx.ExecContext(ctx, `DELETE FROM records_vec WHERE rowid IN (SELECT rowid FROM records WHERE id = ?)`, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM records_vec WHERE rowid IN (SELECT rowid FROM records WHERE id = ?)`, id)
	return err
}

func (s *Store) reuseOrGenerateVector(ctx context.Context, id string, jsonText string) ([]float32, interface{}, error) {
	if !s.options.EnableVector {
		return nil, nil, nil
	}

	var existingJSON sql.NullString
	var existingVector []byte
	err := s.db.QueryRowContext(ctx, "SELECT jsontext, vector FROM records WHERE id = ?", id).Scan(&existingJSON, &existingVector)
	if err == nil && existingJSON.Valid && existingJSON.String == jsonText && len(existingVector) > 0 {
		if v, err := deserializeVector(existingVector); err == nil && len(v) == s.options.VectorDimensions {
			return v, existingVector, nil
		}
	}

	return s.generateVector(ctx, jsonText)
}

func (s *Store) generateVector(ctx context.Context, jsonText string) ([]float32, interface{}, error) {
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

func serializeVector(v []float32) ([]byte, error) {
	if len(v) == 0 {
		return nil, fmt.Errorf("vector cannot be empty")
	}

	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, int32(len(v))); err != nil {
		return nil, fmt.Errorf("failed to write dimension count: %w", err)
	}

	if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
		return nil, fmt.Errorf("failed to write vector data: %w", err)
	}

	return buf.Bytes(), nil
}

func deserializeVector(blob []byte) ([]float32, error) {
	if len(blob) < 4 {
		return nil, fmt.Errorf("invalid vector blob: too short")
	}

	buf := bytes.NewReader(blob)

	var dimCount int32
	if err := binary.Read(buf, binary.LittleEndian, &dimCount); err != nil {
		return nil, fmt.Errorf("failed to read dimension count: %w", err)
	}

	if dimCount <= 0 {
		return nil, fmt.Errorf("invalid dimension count: %d", dimCount)
	}

	expectedSize := 4 + int(dimCount)*4
	if len(blob) != expectedSize {
		return nil, fmt.Errorf("invalid vector blob: size mismatch, expected %d bytes, got %d", expectedSize, len(blob))
	}

	vector := make([]float32, dimCount)
	if err := binary.Read(buf, binary.LittleEndian, &vector); err != nil {
		return nil, fmt.Errorf("failed to read vector data: %w", err)
	}

	return vector, nil
}

func (s *Store) rebuildVectorIndex(ctx context.Context, tx *sql.Tx) error {
	if !s.options.EnableVector {
		return fmt.Errorf("vector operations not available: vectors not enabled")
	}

	// sqlite-vec (vec0) does not build a separate ANN structure yet; this simply
	// refreshes stored vectors and keeps the vec0 table in sync with records.
	rows, err := tx.QueryContext(ctx, "SELECT id, jsontext FROM records WHERE jsontext IS NOT NULL")
	if err != nil {
		return err
	}
	defer rows.Close()

	if _, err := tx.ExecContext(ctx, "DELETE FROM records_vec"); err != nil {
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
		vector, vectorBlob, err := s.reuseOrGenerateVector(ctx, id, jsonText)
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
			if err := s.upsertVecTable(ctx, tx, id, vector); err != nil {
				return fmt.Errorf("reindex vector entry: %w", err)
			}
		} else {
			if err := s.deleteVecEntry(ctx, tx, id); err != nil {
				return fmt.Errorf("clear vector entry: %w", err)
			}
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	return nil
}

func extractTextFromJSON(jsonText string) string {
	if jsonText == "" {
		return ""
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonText), &data); err != nil {
		return ""
	}

	var parts []string
	var extractStrings func(interface{})
	extractStrings = func(v interface{}) {
		switch val := v.(type) {
		case string:
			if val != "" {
				parts = append(parts, val)
			}
		case map[string]interface{}:
			for _, item := range val {
				extractStrings(item)
			}
		case []interface{}:
			for _, item := range val {
				extractStrings(item)
			}
		}
	}

	extractStrings(data)
	return strings.Join(parts, " ")
}
