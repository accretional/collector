package collection

import (
	"context"

	pb "github.com/accretional/collector/gen/collector"
)

// FileSystem defines the interface for file operations associated with a collection.
// Allows swapping local disk for cloud storage or memory-based VFS.
type FileSystem interface {
	Save(ctx context.Context, path string, content []byte) error
	Load(ctx context.Context, path string) ([]byte, error)
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, prefix string) ([]string, error)
	Stat(ctx context.Context, path string) (int64, error)
}

// CollectionRepo defines the interface for a collection repository.
type CollectionRepo interface {
	CreateCollection(ctx context.Context, collection *pb.Collection) (*pb.CreateCollectionResponse, error)
	Discover(ctx context.Context, req *pb.DiscoverRequest) (*pb.DiscoverResponse, error)
	Route(ctx context.Context, req *pb.RouteRequest) (*pb.RouteResponse, error)
	SearchCollections(ctx context.Context, req *pb.SearchCollectionsRequest) (*pb.SearchCollectionsResponse, error)
	GetCollection(ctx context.Context, namespace, name string) (*Collection, error)
	UpdateCollectionMetadata(ctx context.Context, namespace, name string, meta *pb.Collection) error
}
