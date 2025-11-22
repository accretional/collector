package registry

import (
	"context"
	"fmt"
	"net"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Example: How to set up a CollectionService server with registry validation

// SetupCollectionServiceWithValidation demonstrates how to create a CollectionService
// server with registry validation enabled.
func SetupCollectionServiceWithValidation(
	ctx context.Context,
	registry *RegistryServer,
	namespace string,
	collectionRepo collection.CollectionRepo,
	address string,
) (*grpc.Server, net.Listener, error) {
	// 1. Register the CollectionService proto with the registry
	err := RegisterCollectionService(ctx, registry, namespace)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to register CollectionService: %w", err)
	}

	// 2. Create gRPC server with validation
	grpcServer := NewServerWithValidation(registry, namespace)

	// 3. Create and register the service implementation
	collectionServer := collection.NewCollectionServer(collectionRepo)
	pb.RegisterCollectionServiceServer(grpcServer, collectionServer)

	// 4. Start listening
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen: %w", err)
	}

	return grpcServer, lis, nil
}

// SetupDispatcherWithValidation demonstrates how to create a CollectiveDispatcher
// server with registry validation enabled.
func SetupDispatcherWithValidation(
	ctx context.Context,
	registry *RegistryServer,
	namespace string,
	dispatcher pb.CollectiveDispatcherServer,
	address string,
) (*grpc.Server, net.Listener, error) {
	// 1. Register the CollectiveDispatcher service
	err := RegisterDispatcherService(ctx, registry, namespace)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to register Dispatcher: %w", err)
	}

	// 2. Create gRPC server with validation
	grpcServer := NewServerWithValidation(registry, namespace)

	// 3. Register the service implementation
	pb.RegisterCollectiveDispatcherServer(grpcServer, dispatcher)

	// 4. Start listening
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen: %w", err)
	}

	return grpcServer, lis, nil
}

// SetupCollectionRepoWithValidation demonstrates how to create a CollectionRepo
// server with registry validation enabled.
func SetupCollectionRepoWithValidation(
	ctx context.Context,
	registry *RegistryServer,
	namespace string,
	repoServer pb.CollectionRepoServer,
	address string,
) (*grpc.Server, net.Listener, error) {
	// 1. Register the CollectionRepo service
	err := RegisterCollectionRepoService(ctx, registry, namespace)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to register CollectionRepo: %w", err)
	}

	// 2. Create gRPC server with validation
	grpcServer := NewServerWithValidation(registry, namespace)

	// 3. Register the service implementation
	pb.RegisterCollectionRepoServer(grpcServer, repoServer)

	// 4. Start listening
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen: %w", err)
	}

	return grpcServer, lis, nil
}

// Helper functions to register services with the registry

// RegisterCollectionService registers the CollectionService with the registry
func RegisterCollectionService(ctx context.Context, registry *RegistryServer, namespace string) error {
	serviceDesc := &descriptorpb.ServiceDescriptorProto{
		Name: stringPtr("CollectionService"),
		Method: []*descriptorpb.MethodDescriptorProto{
			{Name: stringPtr("Create")},
			{Name: stringPtr("Get")},
			{Name: stringPtr("Update")},
			{Name: stringPtr("Delete")},
			{Name: stringPtr("List")},
			{Name: stringPtr("Search")},
			{Name: stringPtr("Batch")},
			{Name: stringPtr("Describe")},
			{Name: stringPtr("Modify")},
			{Name: stringPtr("Meta")},
			{Name: stringPtr("Invoke")},
		},
	}

	_, err := registry.RegisterService(ctx, &pb.RegisterServiceRequest{
		Namespace:         namespace,
		ServiceDescriptor: serviceDesc,
	})
	return err
}

// RegisterDispatcherService registers the CollectiveDispatcher service with the registry
func RegisterDispatcherService(ctx context.Context, registry *RegistryServer, namespace string) error {
	serviceDesc := &descriptorpb.ServiceDescriptorProto{
		Name: stringPtr("CollectiveDispatcher"),
		Method: []*descriptorpb.MethodDescriptorProto{
			{Name: stringPtr("Serve")},
			{Name: stringPtr("Connect")},
			{Name: stringPtr("Dispatch")},
		},
	}

	_, err := registry.RegisterService(ctx, &pb.RegisterServiceRequest{
		Namespace:         namespace,
		ServiceDescriptor: serviceDesc,
	})
	return err
}

// RegisterCollectionRepoService registers the CollectionRepo service with the registry
func RegisterCollectionRepoService(ctx context.Context, registry *RegistryServer, namespace string) error {
	serviceDesc := &descriptorpb.ServiceDescriptorProto{
		Name: stringPtr("CollectionRepo"),
		Method: []*descriptorpb.MethodDescriptorProto{
			{Name: stringPtr("CreateCollection")},
			{Name: stringPtr("Discover")},
			{Name: stringPtr("Route")},
			{Name: stringPtr("SearchCollections")},
		},
	}

	_, err := registry.RegisterService(ctx, &pb.RegisterServiceRequest{
		Namespace:         namespace,
		ServiceDescriptor: serviceDesc,
	})
	return err
}

func stringPtr(s string) *string {
	return &s
}
