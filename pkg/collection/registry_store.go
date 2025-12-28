package collection

import (
	"context"

	pb "github.com/accretional/collector/gen/collector"
)

// CollectionMetadata holds collection information with its storage path.
type CollectionMetadata struct {
	Collection *pb.Collection
	DBPath     string // Relative path: "{namespace}/{name}.db"
}

// RegistryStore defines the interface for persisting collection metadata.
// The production implementation is CollectionRegistryStore (see collection_registry_store.go),
// which stores collection metadata in the self-hosted system/collections collection.
type RegistryStore interface {
	SaveCollection(ctx context.Context, collection *pb.Collection, dbPath string) error
	GetCollection(ctx context.Context, namespace, name string) (*CollectionMetadata, error)
	ListCollections(ctx context.Context, namespace string) ([]*CollectionMetadata, error)
	DeleteCollection(ctx context.Context, namespace, name string) error
	UpdateMetadata(ctx context.Context, namespace, name string, meta *pb.Collection) error
	Close() error
}
