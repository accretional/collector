package collection

import (
	"context"
	"io"

	pb "github.com/accretional/collector/gen/collector"
)

// Store defines the interface for the underlying database (e.g., SQLite).
// It decouples the Collection from the specific SQL implementation.
type Store interface {
	// Lifecycle
	Close() error
	Path() string // Returns the physical location of the store (for transport)

	// CRUD
	CreateRecord(ctx context.Context, record *pb.CollectionRecord) error
	GetRecord(ctx context.Context, id string) (*pb.CollectionRecord, error)
	UpdateRecord(ctx context.Context, record *pb.CollectionRecord) error
	DeleteRecord(ctx context.Context, id string) error
	ListRecords(ctx context.Context, offset, limit int) ([]*pb.CollectionRecord, error)
	CountRecords(ctx context.Context) (int64, error)

	// Search
	// The store implementation handles translating these generic queries into SQL/FTS/Vector logic.
	Search(ctx context.Context, query *SearchQuery) ([]*SearchResult, error)

	// Maintenance
	Checkpoint(ctx context.Context) error
}

// FileSystem defines the interface for file operations associated with a collection.
// This allows swapping out the local disk for cloud storage or custom VFS.
type FileSystem interface {
	Save(ctx context.Context, path string, content []byte) error
	Load(ctx context.Context, path string) ([]byte, error)
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, prefix string) ([]string, error)
	Stat(ctx context.Context, path string) (int64, error)
}

// SearchQuery is the generic query structure passed to the Store.
type SearchQuery struct {
	FullText     string
	Filters      map[string]Filter // Field path -> Filter
	Vector       []float32         // For vector similarity search
	SimilarityThreshold float32
	Limit        int
	Offset       int
	OrderBy      string
	Ascending    bool
}

type SearchResult struct {
	Record *pb.CollectionRecord
	Score  float64
	Distance float64 // For vector search
}

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
	OpContains     FilterOperator = "CONTAINS"
	OpExists       FilterOperator = "EXISTS"
)
