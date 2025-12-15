package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Search routes queries to either the vector-backed path or the scalar/FTS path.
func (s *SqliteStore) Search(ctx context.Context, q *collection.SearchQuery) ([]*collection.SearchResult, error) {
	if len(q.Vector) > 0 && s.options.EnableVector {
		return s.searchWithVectorIndex(ctx, q)
	}

	var query strings.Builder
	var args []interface{}
	var whereClauses []string

	// Base query
	query.WriteString(`SELECT r.id, r.proto_data, r.data_uri, r.created_at, r.updated_at, r.labels `)
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

	// Label filters (stored as JSON in labels column).
	for key, value := range q.LabelFilters {
		whereClauses = append(whereClauses, fmt.Sprintf(`json_extract(r.labels, '$.%s') = ?`, key))
		args = append(args, value)
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
		var dataURI sql.NullString
		var createdAt, updatedAt int64
		var labelsJSON string
		var score sql.NullFloat64

		var scanArgs = []any{&r.Id, &r.ProtoData, &dataURI, &createdAt, &updatedAt, &labelsJSON}
		if q.FullText != "" {
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

		searchResult := &collection.SearchResult{Record: &r}
		if score.Valid {
			searchResult.Score = score.Float64
		}
		results = append(results, searchResult)
	}
	return results, nil
}

func (s *SqliteStore) searchWithVectorIndex(ctx context.Context, q *collection.SearchQuery) ([]*collection.SearchResult, error) {
	if len(q.Vector) != s.options.VectorDimensions {
		return nil, fmt.Errorf("query vector dimension mismatch: got %d, expected %d", len(q.Vector), s.options.VectorDimensions)
	}

	vectorLiteral := formatVectorLiteral(q.Vector)
	var query strings.Builder
	var args []interface{}
	var whereClauses []string

	query.WriteString(`SELECT r.id, r.proto_data, r.data_uri, r.created_at, r.updated_at, r.labels, v.distance `)
	if q.FullText != "" {
		query.WriteString(`, bm25(records_fts) as score `)
	}
	query.WriteString(`FROM records_vss v JOIN records r ON r.rowid = v.rowid `)
	if q.FullText != "" {
		query.WriteString(`JOIN records_fts fts ON r.rowid = fts.rowid `)
	}

	whereClauses = append(whereClauses, `v.vector MATCH ?`)
	args = append(args, vectorLiteral)

	if q.FullText != "" {
		whereClauses = append(whereClauses, `records_fts MATCH ?`)
		args = append(args, q.FullText)
	}

	for key, filter := range q.Filters {
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

	for key, value := range q.LabelFilters {
		whereClauses = append(whereClauses, fmt.Sprintf(`json_extract(r.labels, '$.%s') = ?`, key))
		args = append(args, value)
	}

	if len(whereClauses) > 0 {
		query.WriteString("WHERE " + strings.Join(whereClauses, " AND "))
	}

	if q.OrderBy != "" {
		order := "ASC"
		if !q.Ascending {
			order = "DESC"
		}
		query.WriteString(fmt.Sprintf(` ORDER BY json_extract(r.jsontext, '$.%s') %s, v.distance`, q.OrderBy, order))
	} else {
		query.WriteString(" ORDER BY v.distance")
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	query.WriteString(" LIMIT ?")
	args = append(args, limit)

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
		var dataURI sql.NullString
		var createdAt, updatedAt int64
		var labelsJSON string
		var distance float64
		var score sql.NullFloat64

		scanArgs := []any{&r.Id, &r.ProtoData, &dataURI, &createdAt, &updatedAt, &labelsJSON, &distance}
		if q.FullText != "" {
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

		result := &collection.SearchResult{
			Record:   &r,
			Distance: distance,
		}

		if q.SimilarityThreshold > 0 {
			maxDistance := (1 / float64(q.SimilarityThreshold)) - 1
			if distance > maxDistance {
				continue
			}
		}

		if score.Valid {
			result.Score = score.Float64
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func enableVectorIndex(db *sql.DB, dims int) error {
	if _, err := db.Exec("SELECT vss_version()"); err != nil {
		return fmt.Errorf("sqlite vector extension unavailable: %w", err)
	}

	stmt := fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS records_vss USING vss0(vector FLOAT[%d]);`, dims)
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("create vss table: %w", err)
	}

	// Use HNSW index for efficient search.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS records_vss_hnsw ON records_vss(vss_hnsw(vector));`); err != nil {
		return fmt.Errorf("create hnsw index: %w", err)
	}

	return nil
}

func formatVectorLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = strconv.FormatFloat(float64(f), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func (s *SqliteStore) upsertVectorIndex(ctx context.Context, exec execContext, id string, vector []float32) error {
	if len(vector) != s.options.VectorDimensions {
		return fmt.Errorf("vector dimension mismatch: got %d, expected %d", len(vector), s.options.VectorDimensions)
	}

	literal := formatVectorLiteral(vector)

	if _, err := exec.ExecContext(ctx, `DELETE FROM records_vss WHERE rowid IN (SELECT rowid FROM records WHERE id = ?)`, id); err != nil {
		return err
	}

	query := fmt.Sprintf(`INSERT INTO records_vss(rowid, vector) SELECT rowid, %s FROM records WHERE id = ?`, literal)
	_, err := exec.ExecContext(ctx, query, id)
	return err
}

func (s *SqliteStore) deleteVectorIndex(ctx context.Context, exec execContext, id string) error {
	_, err := exec.ExecContext(ctx, `DELETE FROM records_vss WHERE rowid IN (SELECT rowid FROM records WHERE id = ?)`, id)
	return err
}

func serializeVector(v []float32) ([]byte, error) {
	if len(v) == 0 {
		return nil, fmt.Errorf("vector cannot be empty")
	}

	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, int32(len(v))); err != nil {
		return nil, fmt.Errorf("failed to write dimension count: %w", err)
	}

	// Write float32 array
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
