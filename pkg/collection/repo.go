package collection

import (
	"context"
	"fmt"
	"sync"
	"github.com/accretional/collector/pkg/db/sqlite"
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


// CollectionRepo is a facade that provides a simple interface for managing collections.
// It uses a CollectionRepoService and a SqliteStore to do the heavy lifting.
type CollectionRepo struct {
	service *CollectionRepoService
	store   *sqlite.SqliteStore
	mu      sync.RWMutex
}

// NewCollectionRepo creates a new CollectionRepo.
func NewCollectionRepo(dbPath string) (*CollectionRepo, error) {
	opts := Options{
		EnableFTS:  true,
		EnableJSON: true,
	}
	store, err := sqlite.NewSqliteStore(dbPath, opts)
	if err != nil {
		return nil, fmt.Errorf("could not create sqlite store: %w", err)
	}

	service := NewCollectionRepoService(store)

	return &CollectionRepo{
		service: service,
		store:   store,
	}, nil
}

// CreateCollection creates a new collection.
func (r *CollectionRepo) CreateCollection(ctx context.Context, collection *pb.Collection) (*pb.CreateCollectionResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.service.CreateCollection(ctx, collection)
}

// Discover finds collections based on the provided criteria.
func (r *CollectionRepo) Discover(ctx context.Context, req *pb.DiscoverRequest) (*pb.DiscoverResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.service.Discover(ctx, req)
}

// Route directs a request to the appropriate collection server.
func (r *CollectionRepo) Route(ctx context.Context, req *pb.RouteRequest) (*pb.RouteResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.service.Route(ctx, req)
}

// SearchCollections searches across multiple collections.
func (r *CollectionRepo) SearchCollections(ctx context.Context, req *pb.SearchCollectionsRequest) (*pb.SearchCollectionsResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.service.SearchCollections(ctx, req)
}
