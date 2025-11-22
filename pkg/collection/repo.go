package collection

import (
	"context"
	"fmt"
	"sync"
	pb "github.com/accretional/collector/gen/collector"
)

// Store defines the interface for the underlying database.
// Implementations (like SQLite) handle the specifics of query translation and storage.
type Store interface {
	// Lifecycle
	Close() error
	// Path returns the physical location of the store (useful for transport/backup).
	Path() string

	// CRUD
	CreateRecord(ctx context.Context, record *pb.CollectionRecord) error
	GetRecord(ctx context.Context, id string) (*pb.CollectionRecord, error)
	UpdateRecord(ctx context.Context, record *pb.CollectionRecord) error
	DeleteRecord(ctx context.Context, id string) error
	ListRecords(ctx context.Context, offset, limit int) ([]*pb.CollectionRecord, error)
	CountRecords(ctx context.Context) (int64, error)

	// Search
	// The store implementation handles translating generic queries into SQL/FTS/Vector logic.
	Search(ctx context.Context, query *SearchQuery) ([]*SearchResult, error)

	// Maintenance
	Checkpoint(ctx context.Context) error
	ReIndex(ctx context.Context) error

	// ExecuteRaw allows lower-level operations required for advanced features
	// like backup (VACUUM INTO) or combination (ATTACH DATABASE).
	ExecuteRaw(query string, args ...interface{}) error
}


// DefaultCollectionRepo is a facade that provides a simple interface for managing collections.
// It uses a CollectionRepoService and a Store to do the heavy lifting.
type DefaultCollectionRepo struct {
	service     *CollectionRepoService
	store       Store
	collections map[string]*pb.Collection // Track created collections by namespace/name
	mu          sync.RWMutex
}

// NewCollectionRepo creates a new DefaultCollectionRepo with the given Store.
func NewCollectionRepo(store Store) *DefaultCollectionRepo {
	service := NewCollectionRepoService(store)

	return &DefaultCollectionRepo{
		service:     service,
		store:       store,
		collections: make(map[string]*pb.Collection),
	}
}

// CreateCollection creates a new collection.
func (r *DefaultCollectionRepo) CreateCollection(ctx context.Context, collection *pb.Collection) (*pb.CreateCollectionResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	resp, err := r.service.CreateCollection(ctx, collection)
	if err != nil {
		return nil, err
	}

	// Track the collection
	key := collection.Namespace + "/" + collection.Name
	r.collections[key] = collection

	return resp, nil
}

// Discover finds collections based on the provided criteria.
func (r *DefaultCollectionRepo) Discover(ctx context.Context, req *pb.DiscoverRequest) (*pb.DiscoverResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.service.Discover(ctx, req)
}

// Route directs a request to the appropriate collection server.
func (r *DefaultCollectionRepo) Route(ctx context.Context, req *pb.RouteRequest) (*pb.RouteResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.service.Route(ctx, req)
}

// SearchCollections searches across multiple collections.
func (r *DefaultCollectionRepo) SearchCollections(ctx context.Context, req *pb.SearchCollectionsRequest) (*pb.SearchCollectionsResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.service.SearchCollections(ctx, req)
}

// GetCollection retrieves a Collection instance by namespace and name.
func (r *DefaultCollectionRepo) GetCollection(ctx context.Context, namespace, name string) (*Collection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check if collection exists
	key := namespace + "/" + name
	meta, exists := r.collections[key]
	if !exists {
		return nil, fmt.Errorf("collection %s not found", key)
	}

	// Use a local filesystem implementation
	fs := &LocalFileSystem{Root: ""}

	return NewCollection(meta, r.store, fs)
}

// UpdateCollectionMetadata updates the metadata for an existing collection.
func (r *DefaultCollectionRepo) UpdateCollectionMetadata(ctx context.Context, namespace, name string, meta *pb.Collection) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := namespace + "/" + name
	if _, exists := r.collections[key]; !exists {
		return fmt.Errorf("collection %s not found", key)
	}

	// Update the collection metadata
	r.collections[key] = meta
	return nil
}
