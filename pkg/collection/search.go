package collection

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "strings"

    pb "github.com/accretional/collector/gen/collector"
)

// SearchQuery represents a structured search query
type SearchQuery struct {
    // Full-text search query (FTS5 syntax)
    FullText string
    
    // JSONB filters: field path -> filter
    // These operate on the jsontext column
    Filters map[string]Filter
    
    // Label filters: operate on the user-controlled labels column
    LabelFilters map[string]string
    
    // Ordering
    OrderBy   string
    Ascending bool
    
    // Pagination
    Limit  int
    Offset int
}

// Filter represents a JSONB filter condition
type Filter struct {
    Operator FilterOperator
    Value    interface{}
}

type FilterOperator string

const (
    OpEquals       FilterOperator = "="
    OpNotEquals    FilterOperator = "!="
    OpGreaterThan  FilterOperator = ">"
    OpLessThan     FilterOperator = "<"
    OpGreaterEqual FilterOperator = ">="
    OpLessEqual    FilterOperator = "<="
    OpContains     FilterOperator = "CONTAINS"     // String contains
    OpIn           FilterOperator = "IN"           // Value in array
    OpExists       FilterOperator = "EXISTS"       // Field exists
    OpNotExists    FilterOperator = "NOT_EXISTS"   // Field doesn't exist
)

// SearchResult represents a search result with score
type SearchResult struct {
    Record *pb.CollectionRecord
    Score  float64 // Relevance score from FTS5
}

// Search performs full-text search and JSONB filtering
func (c *Collection) Search(query SearchQuery) ([]*SearchResult, error) {
    if query.Limit <= 0 {
        query.Limit = 100
    }

    // Build the SQL query
    sqlQuery, args, err := c.buildSearchQuery(query)
    if err != nil {
        return nil, fmt.Errorf("failed to build search query: %w", err)
    }

    rows, err := c.db.Query(sqlQuery, args...)
    if err != nil {
        return nil, fmt.Errorf("failed to execute search: %w", err)
    }
    defer rows.Close()

    var results []*SearchResult
    for rows.Next() {
        var (
            id                   string
            protoData            []byte
            jsontext             string
            dataUri              sql.NullString
            createdAt, updatedAt int64
            labelsJSON           string
            score                sql.NullFloat64
        )

        if err := rows.Scan(&id, &protoData, &jsontext, &dataUri, &createdAt, &updatedAt, &labelsJSON, &score); err != nil {
            return nil, fmt.Errorf("failed to scan row: %w", err)
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

        result := &SearchResult{
            Record: record,
            Score:  0.0,
        }
        if score.Valid {
            result.Score = score.Float64
        }

        results = append(results, result)
    }

    return results, rows.Err()
}

// buildSearchQuery constructs the SQL query from SearchQuery
func (c *Collection) buildSearchQuery(query SearchQuery) (string, []interface{}, error) {
    var (
        selectClause = "SELECT r.id, r.proto_data, r.jsontext, r.data_uri, r.created_at, r.updated_at, r.labels"
        fromClause   = "FROM records r"
        whereClause  []string
        args         []interface{}
        orderClause  string
        joinFTS      bool
    )

    // Add FTS5 search if full-text query provided
    if query.FullText != "" {
        joinFTS = true
        fromClause += " INNER JOIN records_fts fts ON r.rowid = fts.rowid"
        whereClause = append(whereClause, "fts.records_fts MATCH ?")
        args = append(args, query.FullText)
        selectClause += ", bm25(fts.records_fts) as score"
    } else {
        selectClause += ", 0.0 as score"
    }

    // Add JSONB filters (operate on jsontext column)
    if len(query.Filters) > 0 {
        for path, filter := range query.Filters {
            condition, filterArgs, err := c.buildFilterCondition(path, filter)
            if err != nil {
                return "", nil, err
            }
            whereClause = append(whereClause, condition)
            args = append(args, filterArgs...)
        }
    }

    // Add label filters (operate on labels column)
    if len(query.LabelFilters) > 0 {
        for key, value := range query.LabelFilters {
            // Use json_extract on the labels column specifically
            whereClause = append(whereClause, "json_extract(r.labels, ?) = ?")
            args = append(args, "$."+key, value)
        }
    }

    // Construct WHERE clause
    var whereSQL string
    if len(whereClause) > 0 {
        whereSQL = " WHERE " + strings.Join(whereClause, " AND ")
    }

    // Construct ORDER BY clause
    if query.OrderBy != "" {
        direction := "DESC"
        if query.Ascending {
            direction = "ASC"
        }
        
        // Check if ordering by a JSON field (operates on jsontext)
        if strings.Contains(query.OrderBy, ".") {
            orderClause = fmt.Sprintf(" ORDER BY json_extract(r.jsontext, '$.%s') %s", query.OrderBy, direction)
        } else if query.OrderBy == "score" && joinFTS {
            // Order by relevance score
            orderClause = " ORDER BY score ASC" // BM25 returns negative scores, lower is better
        } else {
            orderClause = fmt.Sprintf(" ORDER BY r.%s %s", query.OrderBy, direction)
        }
    } else if joinFTS {
        // Default to score ordering for FTS queries
        orderClause = " ORDER BY score ASC"
    }

    // Construct LIMIT and OFFSET
    limitClause := fmt.Sprintf(" LIMIT %d OFFSET %d", query.Limit, query.Offset)

    // Combine all parts
    fullQuery := selectClause + " " + fromClause + whereSQL + orderClause + limitClause

    return fullQuery, args, nil
}

// buildFilterCondition builds a WHERE condition for a JSONB filter
// All JSON operations now work on the jsontext column, not proto_data
func (c *Collection) buildFilterCondition(path string, filter Filter) (string, []interface{}, error) {
    jsonPath := "$." + path
    var condition string
    var args []interface{}

    switch filter.Operator {
    case OpEquals:
        // Operate on jsontext column for JSON extraction
        condition = "json_extract(jsontext, ?) = ?"
        args = []interface{}{jsonPath, filter.Value}
    
    case OpNotEquals:
        condition = "json_extract(jsontext, ?) != ?"
        args = []interface{}{jsonPath, filter.Value}
    
    case OpGreaterThan:
        condition = "CAST(json_extract(jsontext, ?) AS REAL) > ?"
        args = []interface{}{jsonPath, filter.Value}
    
    case OpLessThan:
        condition = "CAST(json_extract(jsontext, ?) AS REAL) < ?"
        args = []interface{}{jsonPath, filter.Value}
    
    case OpGreaterEqual:
        condition = "CAST(json_extract(jsontext, ?) AS REAL) >= ?"
        args = []interface{}{jsonPath, filter.Value}
    
    case OpLessEqual:
        condition = "CAST(json_extract(jsontext, ?) AS REAL) <= ?"
        args = []interface{}{jsonPath, filter.Value}
    
    case OpContains:
        condition = "json_extract(jsontext, ?) LIKE ?"
        args = []interface{}{jsonPath, fmt.Sprintf("%%%v%%", filter.Value)}
    
    case OpIn:
        // Value should be an array
        values, ok := filter.Value.([]interface{})
        if !ok {
            return "", nil, fmt.Errorf("IN operator requires array value")
        }
        placeholders := make([]string, len(values))
        for i := range values {
            placeholders[i] = "?"
            args = append(args, values[i])
        }
        condition = fmt.Sprintf("json_extract(jsontext, '%s') IN (%s)", jsonPath, strings.Join(placeholders, ","))
    
    case OpExists:
        condition = "json_extract(jsontext, ?) IS NOT NULL"
        args = []interface{}{jsonPath}
    
    case OpNotExists:
        condition = "json_extract(jsontext, ?) IS NULL"
        args = []interface{}{jsonPath}
    
    default:
        return "", nil, fmt.Errorf("unsupported operator: %s", filter.Operator)
    }

    return condition, args, nil
}

// updateFTSIndex updates the FTS5 index for a record
// Now accepts jsontext directly instead of proto bytes
func (c *Collection) updateFTSIndex(id string, jsontext string) error {
    // Parse JSON and flatten to searchable text
    var jsonData map[string]interface{}
    if err := json.Unmarshal([]byte(jsontext), &jsonData); err != nil {
        // If not valid JSON, use the raw text
        jsonData = map[string]interface{}{"_content": jsontext}
    }

    // Flatten JSON to searchable text
    searchText := c.flattenJSON(jsonData)

    // Delete old entry
    _, err := c.db.Exec("DELETE FROM records_fts WHERE id = ?", id)
    if err != nil {
        return fmt.Errorf("failed to delete from FTS: %w", err)
    }

    // Insert new entry
    _, err = c.db.Exec("INSERT INTO records_fts(id, content) VALUES (?, ?)", id, searchText)
    if err != nil {
        return fmt.Errorf("failed to insert into FTS: %w", err)
    }

    return nil
}

// flattenJSON converts a JSON object to searchable text
func (c *Collection) flattenJSON(data map[string]interface{}) string {
    var parts []string
    c.flattenJSONRecursive(data, "", &parts)
    return strings.Join(parts, " ")
}

func (c *Collection) flattenJSONRecursive(data map[string]interface{}, prefix string, parts *[]string) {
    for key, value := range data {
        fullKey := key
        if prefix != "" {
            fullKey = prefix + "." + key
        }

        switch v := value.(type) {
        case string:
            *parts = append(*parts, v)
        case float64, int, int64, bool:
            *parts = append(*parts, fmt.Sprintf("%v", v))
        case map[string]interface{}:
            c.flattenJSONRecursive(v, fullKey, parts)
        case []interface{}:
            for _, item := range v {
                if m, ok := item.(map[string]interface{}); ok {
                    c.flattenJSONRecursive(m, fullKey, parts)
                } else {
                    *parts = append(*parts, fmt.Sprintf("%v", item))
                }
            }
        }
    }
}

// deleteFTSIndex removes a record from the FTS5 index
func (c *Collection) deleteFTSIndex(id string) error {
    _, err := c.db.Exec("DELETE FROM records_fts WHERE id = ?", id)
    return err
}
