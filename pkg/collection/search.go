package collection

import (
	"fmt"

	pb "github.com/accretional/collector/gen/collector"
)

// SearchQuery is the generic query structure passed to the Store.
type SearchQuery struct {
	FullText            string
	Filters             map[string]Filter // Field path -> Filter
	LabelFilters        map[string]string
	Vector              []float32 // For vector similarity search
	SimilarityThreshold float32
	Limit               int
	Offset              int
	OrderBy             string
	Ascending           bool
}

// SearchResult represents a search hit with relevance info.
type SearchResult struct {
	Record   *pb.CollectionRecord
	Score    float64
	Distance float64 // For vector search
}

// Filter represents a condition on a structured field.
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
	OpContains     FilterOperator = "CONTAINS"
	OpIn           FilterOperator = "IN"
	OpExists       FilterOperator = "EXISTS"
	OpNotExists    FilterOperator = "NOT_EXISTS"
)

func IsQueryCompatible(query *SearchQuery, caps SearchCapabilities) error {
	if query.FullText != "" && !caps.FTSEnabled {
		return fmt.Errorf("full-text search requested but FTS is not enabled")
	}

	hasJSONFilters := len(query.Filters) > 0 || len(query.LabelFilters) > 0
	if hasJSONFilters && !caps.JSONEnabled {
		return fmt.Errorf("JSON filters requested but JSON filtering is not enabled")
	}

	if len(query.Vector) > 0 {
		if !caps.VectorSearchEnabled {
			return fmt.Errorf("vector search requested but vector search is not enabled")
		}
		if caps.VectorDimensions <= 0 {
			return fmt.Errorf("vector search requested but vector dimensions are not configured")
		}
		if len(query.Vector) != caps.VectorDimensions {
			return fmt.Errorf("query vector dimension mismatch: got %d, expected %d", len(query.Vector), caps.VectorDimensions)
		}
	}

	return nil
}
