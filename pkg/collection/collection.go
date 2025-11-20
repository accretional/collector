package collection

import (
	"context"
	"fmt"
	"time"

	pb "github.com/accretional/collector/gen/collector"
)

// Options configures the feature set for a Collection.
type Options struct {
	EnableFTS        bool
	EnableJSON       bool // Enable JSON indexing/extraction
	EnableVector     bool // Enable vector search capabilities
	VectorDimensions int
}

// Collection is the domain entity handling logic, validation, and high-level operations.
type Collection struct {
	Meta  *pb.Collection // Metadata state
	Store Store          // The persistence layer
	FS    FileSystem     // The filesystem layer
}

// NewCollection initializes a Collection with provided implementations.
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

// CreateRecord adds a new record to the collection.
func (c *Collection) CreateRecord(ctx context.Context, record *pb.CollectionRecord) error {
	if record.Id == "" {
		return fmt.Errorf("record id required")
	}

	now := time.Now().Unix()
	if record.Metadata == nil {
		record.Metadata = &pb.Metadata{}
	}
	if record.Metadata.CreatedAt == nil {
		record.Metadata.CreatedAt = &pb.Timestamp{Seconds: now}
	}
	record.Metadata.UpdatedAt = &pb.Timestamp{Seconds: now}

	return c.Store.CreateRecord(ctx, record)
}

// GetRecord retrieves a record by ID.
func (c *Collection) GetRecord(ctx context.Context, id string) (*pb.CollectionRecord, error) {
	return c.Store.GetRecord(ctx, id)
}

// UpdateRecord updates an existing record.
func (c *Collection) UpdateRecord(ctx context.Context, record *pb.CollectionRecord) error {
	if record.Id == "" {
		return fmt.Errorf("record id required")
	}
	
	// Update timestamp
	record.Metadata.UpdatedAt = &pb.Timestamp{Seconds: time.Now().Unix()}
	
	return c.Store.UpdateRecord(ctx, record)
}

// DeleteRecord removes a record.
func (c *Collection) DeleteRecord(ctx context.Context, id string) error {
	return c.Store.DeleteRecord(ctx, id)
}

// ListRecords returns paginated records.
func (c *Collection) ListRecords(ctx context.Context, offset, limit int) ([]*pb.CollectionRecord, error) {
	return c.Store.ListRecords(ctx, offset, limit)
}

// Search performs a query against the store.
func (c *Collection) Search(ctx context.Context, query *SearchQuery) ([]*SearchResult, error) {
	return c.Store.Search(ctx, query)
}

// Close closes the underlying store.
func (c *Collection) Close() error {
	return c.Store.Close()
}

// GetNamespace returns the collection namespace.
func (c *Collection) GetNamespace() string {
	return c.Meta.Namespace
}

// GetName returns the collection name.
func (c *Collection) GetName() string {
	return c.Meta.Name
}
