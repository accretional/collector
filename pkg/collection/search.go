package collection

import (
	pb "github.com/accretional/collector/gen/collector"
)

// SearchQuery is the generic query structure passed to the Store.
type SearchQuery struct {
	FullText            string
	Filters             []Filter
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

type Filter struct {
	Field    string // "status" or "user.name" for JSON paths, "labels.<key>" for label filters
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
