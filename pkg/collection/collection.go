package collection

import (
	"context"
	"fmt"
	"time"

	pb "github.com/accretional/collector/gen/collector"
)

// Collection is the domain entity handling logic, validation, and high-level operations.
type Collection struct {
	Meta  *pb.Collection // Metadata state (kept in memory or managed externally)
	Store Store          // The persistence layer
	FS    FileSystem     // The filesystem layer
}

// Options configures the collection features.
type Options struct {
	EnableFTS      bool
	EnableJSON     bool // Enable JSON indexing/extraction
	EnableVector   bool // Enable vector search capabilities
	VectorDimensions int
}

// NewCollection initializes a Collection with provided implementations.
// Note: specific SQLite initialization happens in the factory/caller, not here.
func NewCollection(meta *pb.Collection, store Store, fs FileSystem) (*Collection, error) {
	if meta.Namespace == "" || meta.Name == "" {
		return nil, fmt.Errorf("namespace and name are required")
	}

	// Initialize metadata timestamps if new
	now := time.Now()
	if meta.Metadata == nil {
		meta.Metadata = &pb.Metadata{
			CreatedAt: &pb.Timestamp{Seconds: now.Unix()},
			UpdatedAt: &pb.Timestamp{Seconds: now.Unix()},
		}
	}

	return &Collection{
		Meta:  meta,
		Store: store,
		FS:    fs,
	}, nil
}

func (c *Collection) Create(ctx context.Context, record *pb.CollectionRecord) error {
	// Validate record or enforce schema constraints here if needed
	if record.Id == "" {
		return fmt.Errorf("record id required")
	}
	
	now := time.Now().Unix()
	if record.Metadata == nil {
		record.Metadata = &pb.Metadata{}
	}
	record.Metadata.CreatedAt = &pb.Timestamp{Seconds: now}
	record.Metadata.UpdatedAt = &pb.Timestamp{Seconds: now}

	return c.Store.CreateRecord(ctx, record)
}

func (c *Collection) Get(ctx context.Context, id string) (*pb.CollectionRecord, error) {
	return c.Store.GetRecord(ctx, id)
}

func (c *Collection) Search(ctx context.Context, query *SearchQuery) ([]*SearchResult, error) {
	return c.Store.Search(ctx, query)
}

func (c *Collection) Close() error {
	return c.Store.Close()
}
