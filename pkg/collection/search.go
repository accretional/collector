package collection

import (
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
