package main

import (
	"context"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime/debug"
	"syscall"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/realip"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/testing/testpb"
	"github.com/oklog/run"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	TestServerAddr = "localhost:50052"
	TestDataDir    = "./test-data"
)

// interceptorLogger adapts standard log to interceptor logger
func interceptorLogger() logging.Logger {
	return logging.LoggerFunc(func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
		log.Printf("[%s] %s %v", lvl, msg, fields)
	})
}

func main() {
	logger := log.Default()

	grpcPanicRecoveryHandler := func(p any) (err error) {
		logger.Printf("recovered from panic: %v\n%s", p, debug.Stack())
		return status.Errorf(codes.Internal, "panic triggered: %v", p)
	}

	trustedPeers := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("0.0.0.0/0"), // Allow all for testing
	}
	realIPOpts := []realip.Option{
		realip.WithTrustedPeers(trustedPeers),
		realip.WithHeaders([]string{realip.XForwardedFor, realip.XRealIp}),
	}

	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			realip.UnaryServerInterceptorOpts(realIPOpts...),

			logging.UnaryServerInterceptor(
				interceptorLogger(),
				logging.WithLogOnEvents(logging.StartCall, logging.FinishCall),
			),

			recovery.UnaryServerInterceptor(recovery.WithRecoveryHandler(grpcPanicRecoveryHandler)),
		),
		grpc.ChainStreamInterceptor(
			realip.StreamServerInterceptorOpts(realIPOpts...),
			logging.StreamServerInterceptor(
				interceptorLogger(),
				logging.WithLogOnEvents(logging.StartCall, logging.FinishCall),
			),
			recovery.StreamServerInterceptor(recovery.WithRecoveryHandler(grpcPanicRecoveryHandler)),
		),
	)

	// Register test service
	t := &testpb.TestPingService{}
	testpb.RegisterTestServiceServer(grpcSrv, t)

	// Setup CollectionService for CRUD operations
	if err := setupCollectionService(grpcSrv); err != nil {
		log.Fatalf("failed to setup collection service: %v", err)
	}

	lis, err := net.Listen("tcp", TestServerAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	logger.Printf("Test gRPC server listening on %s", TestServerAddr)
	logger.Println("Server middleware enabled:")
	logger.Println("  - RealIP extraction")
	logger.Println("  - Structured logging")
	logger.Println("  - Panic recovery")
	logger.Println("Services registered:")
	logger.Println("  - TestService (testpb)")
	logger.Println("  - CollectionService (CRUD operations)")
	logger.Printf("Data directory: %s", TestDataDir)
	logger.Println("\nPress Ctrl+C to shutdown")

	g := &run.Group{}
	g.Add(func() error {
		return grpcSrv.Serve(lis)
	}, func(err error) {
		logger.Println("Shutting down server...")
		grpcSrv.GracefulStop()
		grpcSrv.Stop()
	})

	g.Add(run.SignalHandler(context.Background(), syscall.SIGINT, syscall.SIGTERM))

	if err := g.Run(); err != nil {
		logger.Printf("Server error: %v", err)
	}
}

// setupCollectionService creates a minimal CollectionService for testing
func setupCollectionService(grpcSrv *grpc.Server) error {
	ctx := context.Background()

	// Create test data directory
	if err := os.MkdirAll(TestDataDir, 0755); err != nil {
		return err
	}

	// Create path config
	pathConfig := collection.NewPathConfig(TestDataDir)

	// Create a minimal registry store for collection metadata
	// Use in-memory store for the registry itself
	registryStoreDB, err := sqlite.NewStore(":memory:", collection.Options{
		EnableJSON: true,
		EnableFTS:  false, // Disable FTS for test server (no build tag needed)
	})
	if err != nil {
		return err
	}

	// Create a minimal filesystem for registry
	registryFilesPath := filepath.Join(TestDataDir, "registry", "files")
	if err := os.MkdirAll(registryFilesPath, 0755); err != nil {
		return err
	}
	registryFS, err := collection.NewLocalFileSystem(registryFilesPath)
	if err != nil {
		return err
	}

	// Create registry store
	registryStore, err := collection.NewCollectionRegistryStoreFromStore(registryStoreDB, registryFS)
	if err != nil {
		return err
	}

	// Create a dummy store for the repo (not really used, but required)
	dummyStore, err := sqlite.NewStore(":memory:", collection.Options{
		EnableJSON: true,
		EnableFTS:  false,
	})
	if err != nil {
		return err
	}

	// Create store factory
	storeFactory := func(path string, opts collection.Options) (collection.Store, error) {
		// Ensure FTS is disabled for test server (no build tag)
		opts.EnableFTS = false
		return sqlite.NewStore(path, opts)
	}

	// Create collection repo
	collectionRepo := collection.NewCollectionRepo(dummyStore, pathConfig, registryStore, storeFactory)

	// Create and register CollectionService
	collectionServer := collection.NewCollectionServer(collectionRepo)
	pb.RegisterCollectionServiceServer(grpcSrv, collectionServer)

	// Create a test collection for easy testing
	testCollection := &pb.Collection{
		Namespace: "shared",
		Name:      "test",
		MessageType: &pb.MessageTypeRef{
			Namespace:   "test",
			MessageName: "TestItem",
		},
	}

	// Create the collection via repo (this will create the DB and filesystem)
	_, err = collectionRepo.CreateCollection(ctx, testCollection)
	if err != nil {
		// Ignore error if collection already exists
		log.Printf("Note: test collection may already exist: %v", err)
	}

	log.Printf("✓ CollectionService ready with test collection: shared/test")

	return nil
}
