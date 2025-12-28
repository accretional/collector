package collection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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

	// Backup creates an online backup of the database to the specified path.
	// This should be implemented in a WAL-friendly way to allow concurrent access.
	Backup(ctx context.Context, destPath string) error

	// ExecuteRaw allows lower-level operations required for advanced features
	// like backup (VACUUM INTO) or combination (ATTACH DATABASE).
	ExecuteRaw(query string, args ...interface{}) error
}

// JSONConverterSetter is an optional interface that stores can implement
// to support setting a JSON converter for proto→JSON conversion.
type JSONConverterSetter interface {
	SetJSONConverter(conv ProtoToJSONConverter)
}

// StoreFactory is a function that creates a new Store instance at the given path with the given options.
type StoreFactory func(path string, opts Options) (Store, error)

// DefaultCollectionRepo is a facade that provides a simple interface for managing collections.
// It uses a CollectionRepoService and a Store to do the heavy lifting.
type DefaultCollectionRepo struct {
	service          *CollectionRepoService
	store            Store
	pathConfig       *PathConfig
	storeFactory     StoreFactory
	typeValidator    MessageTypeValidator   // Optional: validates message types if set
	converterFactory JSONConverterFactory   // Optional: creates JSON converters for stores
}

// NewCollectionRepo creates a new DefaultCollectionRepo with the given Store, PathConfig, RegistryStore, and StoreFactory.
func NewCollectionRepo(store Store, pathConfig *PathConfig, registryStore RegistryStore, storeFactory StoreFactory) *DefaultCollectionRepo {
	service := NewCollectionRepoService(store, registryStore)

	return &DefaultCollectionRepo{
		service:      service,
		store:        store,
		pathConfig:   pathConfig,
		storeFactory: storeFactory,
	}
}

// SetTypeValidator sets the message type validator for this repo.
// This is optional - if not set, type validation is skipped.
func (r *DefaultCollectionRepo) SetTypeValidator(validator MessageTypeValidator) {
	r.typeValidator = validator
}

// SetJSONConverterFactory sets the factory used to create JSON converters for stores.
// If not set, stores will use fallback behavior (proto_data if valid JSON, else "{}").
func (r *DefaultCollectionRepo) SetJSONConverterFactory(factory JSONConverterFactory) {
	r.converterFactory = factory
}

// CreateCollection creates a new collection.
func (r *DefaultCollectionRepo) CreateCollection(ctx context.Context, collection *pb.Collection) (*pb.CreateCollectionResponse, error) {
	// Validate input
	if collection == nil {
		return nil, fmt.Errorf("collection cannot be nil")
	}

	// Validate namespace and collection name (defense in depth - also validated in service)
	if err := ValidateNamespace(collection.Namespace); err != nil {
		return nil, fmt.Errorf("invalid namespace: %w", err)
	}
	if err := ValidateCollectionName(collection.Name); err != nil {
		return nil, fmt.Errorf("invalid collection name: %w", err)
	}

	// Validate message type if validator is configured
	if r.typeValidator != nil {
		if err := r.typeValidator.ValidateCollectionMessageType(ctx, collection); err != nil {
			return nil, fmt.Errorf("invalid message type: %w", err)
		}
	}

	// Create the database file and files directory first
	dbPath, err := r.pathConfig.CollectionDBPath(collection.Namespace, collection.Name)
	if err != nil {
		return nil, fmt.Errorf("invalid collection path: %w", err)
	}
	filesPath, err := r.pathConfig.CollectionFilesPath(collection.Namespace, collection.Name)
	if err != nil {
		return nil, fmt.Errorf("invalid collection files path: %w", err)
	}

	// Ensure directories exist
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}
	if err := os.MkdirAll(filesPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create files directory: %w", err)
	}

	// Create the database file by opening it with the store factory
	store, err := r.storeFactory(dbPath, Options{EnableJSON: true, EnableFTS: true})
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}
	// Close the store immediately - we just needed to create the file
	store.Close()

	// Now register the collection metadata
	return r.service.CreateCollection(ctx, collection)
}

// DeleteCollection deletes a collection and all its data.
func (r *DefaultCollectionRepo) DeleteCollection(ctx context.Context, req *pb.DeleteCollectionRequest) (*pb.DeleteCollectionResponse, error) {
	if req == nil || req.Collection == nil {
		return nil, fmt.Errorf("request and collection cannot be nil")
	}

	namespace := req.Collection.Namespace
	name := req.Collection.Name

	// Validate namespace and collection name
	if err := ValidateNamespace(namespace); err != nil {
		return nil, fmt.Errorf("invalid namespace: %w", err)
	}
	if err := ValidateCollectionName(name); err != nil {
		return nil, fmt.Errorf("invalid collection name: %w", err)
	}

	// Get the collection to check for active operations
	collection, err := r.GetCollection(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("collection not found: %w", err)
	}

	// Check for active operations (backup, restore, clone)
	if err := CheckOperationConflict(collection.Meta); err != nil {
		return nil, fmt.Errorf("cannot delete: %w", err)
	}

	// Register delete operation
	deleteURI := fmt.Sprintf("delete:%s/%s", namespace, name)
	if err := StartOperation(ctx, r, namespace, name, "delete", deleteURI, r.pathConfig.DataDir, DeleteTimeout); err != nil {
		return nil, fmt.Errorf("failed to register delete operation: %w", err)
	}

	// Ensure operation state is cleared on completion
	defer func() {
		if err := CompleteOperation(ctx, r, namespace, name); err != nil {
			fmt.Printf("Warning: failed to clear delete operation state: %v\n", err)
		}
	}()

	// Calculate bytes to be freed
	var bytesFreed int64

	// Get database path and size
	dbPath, err := r.pathConfig.CollectionDBPath(namespace, name)
	if err != nil {
		return nil, fmt.Errorf("invalid collection path: %w", err)
	}
	if info, err := os.Stat(dbPath); err == nil {
		bytesFreed += info.Size()
	}

	// Get files path and calculate directory size
	filesPath, err := r.pathConfig.CollectionFilesPath(namespace, name)
	if err != nil {
		return nil, fmt.Errorf("invalid collection files path: %w", err)
	}
	if info, err := os.Stat(filesPath); err == nil && info.IsDir() {
		// Calculate directory size recursively
		filepath.Walk(filesPath, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				bytesFreed += info.Size()
			}
			return nil
		})
	}

	// Close the collection's store before deleting files
	if err := collection.Close(); err != nil {
		return nil, fmt.Errorf("failed to close collection: %w", err)
	}

	// Delete the database file
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to delete database file: %w", err)
	}

	// Delete the files directory
	if err := os.RemoveAll(filesPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to delete files directory: %w", err)
	}

	// Remove metadata from registry
	if err := r.service.registryStore.DeleteCollection(ctx, namespace, name); err != nil {
		return nil, fmt.Errorf("failed to remove from registry: %w", err)
	}

	return &pb.DeleteCollectionResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: "collection deleted successfully",
		},
		BytesFreed: bytesFreed,
	}, nil
}

// Discover finds collections based on the provided criteria.
func (r *DefaultCollectionRepo) Discover(ctx context.Context, req *pb.DiscoverRequest) (*pb.DiscoverResponse, error) {
	return r.service.Discover(ctx, req)
}

// Route directs a request to the appropriate collection server.
func (r *DefaultCollectionRepo) Route(ctx context.Context, req *pb.RouteRequest) (*pb.RouteResponse, error) {
	return r.service.Route(ctx, req)
}

// SearchCollections searches across multiple collections.
func (r *DefaultCollectionRepo) SearchCollections(ctx context.Context, req *pb.SearchCollectionsRequest) (*pb.SearchCollectionsResponse, error) {
	return r.service.SearchCollections(ctx, req)
}

// GetCollection retrieves a Collection instance by namespace and name.
func (r *DefaultCollectionRepo) GetCollection(ctx context.Context, namespace, name string) (*Collection, error) {
	key := namespace + "/" + name

	// Get metadata from registry
	metadata, err := r.service.registryStore.GetCollection(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("collection %s not found: %w", key, err)
	}

	// Open database at path from registry using the store factory
	dbPath, err := r.pathConfig.CollectionDBPath(namespace, name)
	if err != nil {
		return nil, fmt.Errorf("invalid collection path: %w", err)
	}
	store, err := r.storeFactory(dbPath, Options{EnableJSON: true, EnableFTS: true})
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}

	// Set up JSON converter if factory is available and store supports it
	if r.converterFactory != nil {
		if setter, ok := store.(JSONConverterSetter); ok {
			if mt := metadata.Collection.GetMessageType(); mt != nil {
				if conv := r.converterFactory(mt.GetNamespace(), mt.GetMessageName()); conv != nil {
					setter.SetJSONConverter(conv)
				}
			}
		}
	}

	// Create filesystem
	filesPath, err := r.pathConfig.CollectionFilesPath(namespace, name)
	if err != nil {
		return nil, fmt.Errorf("invalid collection files path: %w", err)
	}
	fs, err := NewLocalFileSystem(filesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem at %s: %w", filesPath, err)
	}

	return NewCollection(metadata.Collection, store, fs)
}

// UpdateCollectionMetadata updates the metadata for an existing collection.
func (r *DefaultCollectionRepo) UpdateCollectionMetadata(ctx context.Context, namespace, name string, meta *pb.Collection) error {
	r.service.mu.Lock()
	defer r.service.mu.Unlock()

	key := namespace + "/" + name
	if _, exists := r.service.collections[key]; !exists {
		return fmt.Errorf("collection %s not found", key)
	}

	// Update the collection metadata
	r.service.collections[key] = meta

	// Persist to registry store if available
	if r.service.registryStore != nil {
		if err := r.service.registryStore.UpdateMetadata(ctx, namespace, name, meta); err != nil {
			return fmt.Errorf("failed to persist metadata update: %w", err)
		}
	}

	return nil
}
