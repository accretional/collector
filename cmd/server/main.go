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

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
	"github.com/accretional/collector/pkg/dispatch"
	"github.com/accretional/collector/pkg/registry"
	"google.golang.org/grpc"
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
	collectionServerPort := 50051
	dispatcherPort := 50052
	repoPort := 50053
	registryPort := 50054

	log.Printf("Starting Collector System (ID: %s, Namespace: %s)", collectorID, namespace)

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
	// 3. Setup Dispatcher with Registry Validation
	// ========================================================================

	validator := registry.NewRegistryValidator(registryServer)
	dispatcher := dispatch.NewDispatcherWithRegistry(
		collectorID,
		fmt.Sprintf("localhost:%d", dispatcherPort),
		[]string{namespace},
		validator,
	)
	log.Printf("✓ Dispatcher created with registry validation")

	// ========================================================================
	// 4. Start Servers
	// ========================================================================

	var wg sync.WaitGroup
	var servers []*grpc.Server

	// Start Registry Server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startRegistryServer(registryServer, registryPort); err != nil {
			log.Printf("Registry server error: %v", err)
		}
	}()
	log.Printf("✓ Registry server started on port %d", registryPort)

	// Start CollectionService Server with validation
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv, lis, err := registry.SetupCollectionServiceWithValidation(
			ctx,
			registryServer,
			namespace,
			collectionRepo,
			fmt.Sprintf("localhost:%d", collectionServerPort),
		)
		if err != nil {
			log.Printf("CollectionService setup error: %v", err)
			return
		}
		servers = append(servers, srv)
		if err := srv.Serve(lis); err != nil {
			log.Printf("CollectionService server error: %v", err)
		}
	}()
	log.Printf("✓ CollectionService server started on port %d (with validation)", collectionServerPort)

	// Start Dispatcher Server with validation
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv, lis, err := registry.SetupDispatcherWithValidation(
			ctx,
			registryServer,
			namespace,
			dispatcher,
			fmt.Sprintf("localhost:%d", dispatcherPort),
		)
		if err != nil {
			log.Printf("Dispatcher setup error: %v", err)
			return
		}
		servers = append(servers, srv)
		if err := srv.Serve(lis); err != nil {
			log.Printf("Dispatcher server error: %v", err)
		}
	}()
	log.Printf("✓ Dispatcher server started on port %d (with validation)", dispatcherPort)

	// Start CollectionRepo Server with validation
	wg.Add(1)
	go func() {
		defer wg.Done()
		repoGrpcServer := collection.NewGrpcServer(collectionRepo)
		srv, lis, err := registry.SetupCollectionRepoWithValidation(
			ctx,
			registryServer,
			namespace,
			repoGrpcServer,
			fmt.Sprintf("localhost:%d", repoPort),
		)
		if err != nil {
			log.Printf("CollectionRepo setup error: %v", err)
			return
		}
		servers = append(servers, srv)
		if err := srv.Serve(lis); err != nil {
			log.Printf("CollectionRepo server error: %v", err)
		}
	}()
	log.Printf("✓ CollectionRepo server started on port %d (with validation)", repoPort)

	// ========================================================================
	// 5. Wait for shutdown signal
	// ========================================================================

	log.Println("\n========================================")
	log.Println("All servers running with registry validation!")
	log.Println("========================================")
	log.Printf("Registry:         localhost:%d", registryPort)
	log.Printf("CollectionService: localhost:%d", collectionServerPort)
	log.Printf("Dispatcher:        localhost:%d", dispatcherPort)
	log.Printf("CollectionRepo:    localhost:%d", repoPort)
	log.Println("========================================")
	log.Println("Press Ctrl+C to shutdown")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("\nShutting down servers...")

	// Graceful shutdown
	for _, srv := range servers {
		srv.GracefulStop()
	}
	dispatcher.Shutdown()

	log.Println("Shutdown complete")
	return nil
}

func startRegistryServer(registryServer *registry.RegistryServer, port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterCollectorRegistryServer(grpcServer, registryServer)

	return grpcServer.Serve(lis)
}
