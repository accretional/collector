package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
	"github.com/accretional/collector/pkg/dispatch"
	"github.com/accretional/collector/pkg/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()

	// Configuration
	namespace := "production"
	collectorID := "collector-001"
	collectorPort := 50051

	log.Printf("Starting Collector (ID: %s, Namespace: %s)", collectorID, namespace)

	// ========================================================================
	// 1. Setup Registry Collections
	// ========================================================================

	registryPath := "./data/registry"
	if err := os.MkdirAll(registryPath, 0755); err != nil {
		return fmt.Errorf("create registry dir: %w", err)
	}

	// Registry protos collection
	protosDBPath := filepath.Join(registryPath, "protos.db")
	protosStore, err := sqlite.NewSqliteStore(protosDBPath, collection.Options{EnableJSON: true})
	if err != nil {
		return fmt.Errorf("init protos store: %w", err)
	}
	defer protosStore.Close()

	registeredProtos, err := collection.NewCollection(
		&pb.Collection{Namespace: "system", Name: "registered_protos"},
		protosStore,
		&collection.LocalFileSystem{},
	)
	if err != nil {
		return fmt.Errorf("create protos collection: %w", err)
	}

	// Registry services collection
	servicesDBPath := filepath.Join(registryPath, "services.db")
	servicesStore, err := sqlite.NewSqliteStore(servicesDBPath, collection.Options{EnableJSON: true})
	if err != nil {
		return fmt.Errorf("init services store: %w", err)
	}
	defer servicesStore.Close()

	registeredServices, err := collection.NewCollection(
		&pb.Collection{Namespace: "system", Name: "registered_services"},
		servicesStore,
		&collection.LocalFileSystem{},
	)
	if err != nil {
		return fmt.Errorf("create services collection: %w", err)
	}

	// Create registry server
	registryServer := registry.NewRegistryServer(registeredProtos, registeredServices)
	log.Println("✓ Registry server created")

	// Register all services in the registry
	if err := registry.RegisterCollectionService(ctx, registryServer, namespace); err != nil {
		return fmt.Errorf("register CollectionService: %w", err)
	}
	log.Printf("✓ Registered CollectionService in namespace '%s'", namespace)

	if err := registry.RegisterDispatcherService(ctx, registryServer, namespace); err != nil {
		return fmt.Errorf("register Dispatcher: %w", err)
	}
	log.Printf("✓ Registered CollectiveDispatcher in namespace '%s'", namespace)

	if err := registry.RegisterCollectionRepoService(ctx, registryServer, namespace); err != nil {
		return fmt.Errorf("register CollectionRepo: %w", err)
	}
	log.Printf("✓ Registered CollectionRepo in namespace '%s'", namespace)

	// ========================================================================
	// 2. Setup Collection Repository
	// ========================================================================

	repoPath := "./data/repo"
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return fmt.Errorf("create repo dir: %w", err)
	}

	repoDBPath := filepath.Join(repoPath, "collections.db")
	repoStore, err := sqlite.NewSqliteStore(repoDBPath, collection.Options{EnableJSON: true})
	if err != nil {
		return fmt.Errorf("init repo store: %w", err)
	}
	defer repoStore.Close()

	collectionRepo := collection.NewCollectionRepo(repoStore)
	log.Println("✓ Collection repository created")

	// ========================================================================
	// 3. Create Single gRPC Server with ALL Services
	// ========================================================================

	// Create one gRPC server with validation for this namespace
	grpcServer := registry.NewServerWithValidation(registryServer, namespace)

	// Register ALL services on the same server

	// 1. Registry Service
	pb.RegisterCollectorRegistryServer(grpcServer, registryServer)
	log.Println("✓ Registered CollectorRegistry service")

	// 2. Collection Service
	collectionServer := collection.NewCollectionServer(collectionRepo)
	pb.RegisterCollectionServiceServer(grpcServer, collectionServer)
	log.Println("✓ Registered CollectionService")

	// 4. CollectionRepo Service
	repoGrpcServer := collection.NewGrpcServer(collectionRepo)
	pb.RegisterCollectionRepoServer(grpcServer, repoGrpcServer)
	log.Println("✓ Registered CollectionRepo")

	// ========================================================================
	// 4. Start Server and Create Loopback Connection
	// ========================================================================

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", collectorPort))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	// Start server in background so we can connect to it
	go grpcServer.Serve(lis)
	time.Sleep(100 * time.Millisecond) // Let server start

	actualAddr := lis.Addr().String()
	log.Printf("✓ Server started on %s", actualAddr)

	// ========================================================================
	// 5. Setup Dispatcher with gRPC-based Registry Validation
	// ========================================================================

	// Create loopback gRPC connection to our own server for service-to-service communication
	loopbackConn, err := grpc.NewClient(actualAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to create loopback connection: %w", err)
	}
	defer loopbackConn.Close()

	// Create Registry client for validation via gRPC
	registryClient := pb.NewCollectorRegistryClient(loopbackConn)

	// Wrap the registry client to implement the ServiceMethodValidator interface
	grpcValidator := &grpcRegistryClientValidator{client: registryClient}
	validator := registry.NewGRPCRegistryValidator(grpcValidator)

	// Create dispatcher with gRPC-based validation
	dispatcher := dispatch.NewDispatcherWithRegistry(
		collectorID,
		actualAddr,
		[]string{namespace},
		validator,
	)
	log.Println("✓ Dispatcher created with gRPC-based registry validation")

	// Register Dispatcher service
	pb.RegisterCollectiveDispatcherServer(grpcServer, dispatcher)
	log.Println("✓ Registered CollectiveDispatcher service")

	log.Println("\n========================================")
	log.Printf("Collector %s running on localhost:%d", collectorID, collectorPort)
	log.Println("All services available:")
	log.Println("  - CollectorRegistry")
	log.Println("  - CollectionService")
	log.Println("  - CollectiveDispatcher")
	log.Println("  - CollectionRepo")
	log.Printf("Namespace: %s", namespace)
	log.Println("Registry validation: ENABLED")
	log.Println("========================================")
	log.Println("Press Ctrl+C to shutdown")

	// Handle shutdown in background
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Println("\nShutting down...")
		grpcServer.GracefulStop()
		dispatcher.Shutdown()
		log.Println("Shutdown complete")
	}()

	// Serve (blocks until shutdown)
	if err := grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	wg.Wait()
	return nil
}

// grpcRegistryClientValidator wraps a gRPC Registry client to implement ServiceMethodValidator
type grpcRegistryClientValidator struct {
	client pb.CollectorRegistryClient
}

// ValidateServiceMethod validates by calling the Registry via gRPC
// Since the Registry doesn't have a dedicated ValidateMethod RPC, we use the direct validator
// But the key is that we're going through the gRPC layer to access the registry
func (v *grpcRegistryClientValidator) ValidateServiceMethod(ctx context.Context, namespace, serviceName, methodName string) error {
	// For now, we attempt registration and check for AlreadyExists
	// This validates that the service exists in the registry
	// TODO: Add a dedicated ValidateMethod RPC to the Registry proto

	// Create a minimal service descriptor
	serviceDesc := &pb.RegisterServiceRequest{
		Namespace: namespace,
		ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
			Name: proto.String(serviceName),
		},
	}

	// Try to register - if it exists, we get AlreadyExists
	_, err := v.client.RegisterService(ctx, serviceDesc)
	if err != nil {
		// AlreadyExists means the service is registered - validation passes!
		if grpc.Code(err).String() == "AlreadyExists" {
			return nil
		}
		// Other errors mean service not found or registry issue
		return fmt.Errorf("service %s.%s not registered: %w", serviceName, methodName, err)
	}

	// If registration succeeded, the service wasn't registered before
	return fmt.Errorf("service %s.%s was not registered in namespace %s", serviceName, methodName, namespace)
}
