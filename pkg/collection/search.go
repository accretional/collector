package collection

import (
	pb "github.com/accretional/collector/gen/collector"
)

// SearchQuery is the generic query structure passed to the Store.
// It supports multiple search modes that can be combined:
//   - FullText: FTS (full-text search) for exact text matching
//   - SemanticText: Semantic search - text to embed and search by meaning
//   - Filters: Structured field filters (JSON path queries)
//   - LabelFilters: Label-based filtering
type SearchQuery struct {
	// Search modes
	FullText     string // FTS search (exact text matching)
	SemanticText string // Semantic search (embedding-based similarity)

	// Filters
	Filters      map[string]Filter // Field path -> Filter
	LabelFilters map[string]string // Label key -> value

	// Vector search options
	SimilarityThreshold float32 // Minimum similarity (0-1), filters results

	// Pagination & sorting
	Limit     int
	Offset    int
	OrderBy   string
	Ascending bool
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
