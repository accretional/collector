package server

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
	"github.com/accretional/collector/pkg/bootstrap"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
	"github.com/accretional/collector/pkg/dispatch"
	"github.com/accretional/collector/pkg/logging"
	"github.com/accretional/collector/pkg/registry"
	"github.com/accretional/collector/pkg/security"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Config holds server configuration options.
//
// Note: The "system" namespace is reserved for internal collections:
//   - system/collections - Collection metadata registry
//   - system/types - Protobuf type registry
//   - system/connections - Collector connections
//   - system/audit - Audit log (future)
//   - system/logs - System logs (structured logging to collection)
//
// Use your own namespace for application data.
type Config struct {
	// DataDir is the root directory for all data storage (default: "./data")
	DataDir string

	// Port is the gRPC server port (default: 50051)
	Port int

	// Namespace is the default namespace for this collector (default: "shared")
	// This namespace is used for service registration and default collection creation.
	// The "system" namespace is reserved for internal collections.
	Namespace string

	// CollectorID is the unique identifier for this collector (default: random UUID7)
	// Used for distributed system coordination and service registration.
	CollectorID string

	// Logger is the logger to use (default: log.Default())
	Logger *log.Logger

	// AuthInterceptor is an optional security interceptor (default: allow all)
	AuthInterceptor grpc.UnaryServerInterceptor
}

// Server represents a fully configured Collector server with all services.
type Server struct {
	config     Config
	logger     *log.Logger
	log        logging.Logger // Structured logger
	grpcServer *grpc.Server
	listener   net.Listener
	dispatcher *dispatch.Dispatcher

	// Components that need cleanup
	systemCollections *bootstrap.SystemCollections
	registryStore     collection.RegistryStore
	loopbackConn      *grpc.ClientConn
	stores            []collection.Store

	shutdownOnce sync.Once
	shutdownChan chan struct{}
}

// New creates a new Collector server with the given configuration.
func New(config Config) (*Server, error) {
	// Set defaults
	if config.DataDir == "" {
		config.DataDir = "./data"
	}
	if config.Port == 0 {
		config.Port = 50051
	}
	if config.Namespace == "" {
		config.Namespace = "shared"
	}
	if config.CollectorID == "" {
		config.CollectorID = uuid.Must(uuid.NewV7()).String()
	}
	if config.Logger == nil {
		config.Logger = log.Default()
	}

	s := &Server{
		config:       config,
		logger:       config.Logger,
		shutdownChan: make(chan struct{}),
	}

	ctx := context.Background()

	s.logger.Printf("Starting Collector (ID: %s, Namespace: %s)", config.CollectorID, config.Namespace)

	// ========================================================================
	// 1. Bootstrap System Collections
	// ========================================================================

	pathConfig := collection.NewPathConfig(config.DataDir)
	s.logger.Printf("Data directory: %s", config.DataDir)
	s.logger.Printf("Backup directory: %s", pathConfig.BackupDir())

	s.logger.Println("========================================")
	s.logger.Println("Bootstrapping system collections...")
	s.logger.Println("========================================")

	systemCollections, err := bootstrap.BootstrapSystemCollections(ctx, pathConfig)
	if err != nil {
		return nil, fmt.Errorf("bootstrap system collections: %w", err)
	}
	s.systemCollections = systemCollections

	// Initialize structured logger
	sysLogger := collection.NewSystemLogger(systemCollections.Logs)
	s.log = sysLogger.With("collector_id", config.CollectorID, "namespace", config.Namespace)

	s.logger.Println("✓ System collections ready:")
	s.logger.Printf("  - Collection Registry: system/collections")
	s.logger.Printf("  - Type Registry: system/types")
	s.logger.Printf("  - Connections: system/connections")
	s.logger.Printf("  - Audit: system/audit")
	s.logger.Printf("  - Logs: system/logs")
	s.logger.Println("========================================")

	// ========================================================================
	// 2. Setup Registry Collections
	// ========================================================================

	registryPath := filepath.Join(config.DataDir, "registry")
	if err := os.MkdirAll(registryPath, 0755); err != nil {
		return nil, fmt.Errorf("create registry dir: %w", err)
	}

	// Registry protos collection
	protosDBPath := filepath.Join(registryPath, "protos.db")
	protosStore, err := sqlite.NewStore(protosDBPath, collection.Options{EnableJSON: true})
	if err != nil {
		return nil, fmt.Errorf("init protos store: %w", err)
	}
	protosStore.SetJSONConverter(collection.GetSystemTypeConverter("RegisteredProto"))
	s.stores = append(s.stores, protosStore)

	registeredProtos, err := collection.NewCollection(
		&pb.Collection{
			Namespace: "system",
			Name:      "registered_protos",
			MessageType: &pb.MessageTypeRef{
				Namespace:   "collector",
				MessageName: "RegisteredProto",
			},
		},
		protosStore,
		&collection.LocalFileSystem{},
	)
	if err != nil {
		return nil, fmt.Errorf("create protos collection: %w", err)
	}

	// Registry services collection
	servicesDBPath := filepath.Join(registryPath, "services.db")
	servicesStore, err := sqlite.NewStore(servicesDBPath, collection.Options{EnableJSON: true})
	if err != nil {
		return nil, fmt.Errorf("init services store: %w", err)
	}
	servicesStore.SetJSONConverter(collection.GetSystemTypeConverter("RegisteredService"))
	s.stores = append(s.stores, servicesStore)

	registeredServices, err := collection.NewCollection(
		&pb.Collection{
			Namespace: "system",
			Name:      "registered_services",
			MessageType: &pb.MessageTypeRef{
				Namespace:   "collector",
				MessageName: "RegisteredService",
			},
		},
		servicesStore,
		&collection.LocalFileSystem{},
	)
	if err != nil {
		return nil, fmt.Errorf("create services collection: %w", err)
	}

	// Create registry server
	registryServer := registry.NewRegistryServer(registeredProtos, registeredServices)

	// Wire type registry into registry server for automatic type registration
	registryServer.SetTypeRegistrar(systemCollections.TypeRegistry)
	s.log.Info("Registry server created with type registration")

	// Register all services in the registry
	if err := registry.RegisterCollectionService(ctx, registryServer, config.Namespace); err != nil {
		return nil, fmt.Errorf("register CollectionService: %w", err)
	}
	s.log.Info("Registered CollectionService", "namespace", config.Namespace)

	if err := registry.RegisterDispatcherService(ctx, registryServer, config.Namespace); err != nil {
		return nil, fmt.Errorf("register Dispatcher: %w", err)
	}
	s.log.Info("Registered CollectiveDispatcher", "namespace", config.Namespace)

	if err := registry.RegisterCollectionRepoService(ctx, registryServer, config.Namespace); err != nil {
		return nil, fmt.Errorf("register CollectionRepo: %w", err)
	}
	s.log.Info("Registered CollectionRepo", "namespace", config.Namespace)

	// ========================================================================
	// 3. Setup Collection Repository
	// ========================================================================

	// Use the system/collections collection for the registry store
	// This enables "dogfooding" - the registry is just another collection
	registryStore := collection.NewCollectionRegistryStore(s.systemCollections.CollectionRegistry)
	s.registryStore = registryStore
	s.log.Info("Registry store initialized using system/collections")

	// Clean up any timed-out operations from previous crashes
	s.log.Info("Checking for timed-out operations...")
	if cleaned, err := collection.CleanupTimedOutOperations(ctx, registryStore); err != nil {
		s.log.Warn("Failed to cleanup timed-out operations", "error", err)
	} else if cleaned > 0 {
		s.log.Info("Cleaned up timed-out operations", "count", cleaned)
	}

	// Create repo with PathConfig and registry store
	dummyStore, err := sqlite.NewStore(":memory:", collection.Options{EnableJSON: true})
	if err != nil {
		return nil, fmt.Errorf("failed to create dummy store: %w", err)
	}
	s.stores = append(s.stores, dummyStore)

	// Create store factory wrapper
	storeFactory := func(path string, opts collection.Options) (collection.Store, error) {
		return sqlite.NewStore(path, opts)
	}

	collectionRepo := collection.NewCollectionRepo(dummyStore, pathConfig, registryStore, storeFactory)

	// Wire type registry into collection repo for type validation
	collectionRepo.SetTypeValidator(systemCollections.TypeRegistry)

	// Wire JSON converter factory for proto→JSON conversion in stores
	// This allows searching collections by JSON fields
	converterFactory := collection.NewRegistryConverterFactory(func(namespace, messageName string) (*descriptorpb.FileDescriptorProto, error) {
		ctx := context.Background()
		registeredProto, err := registryServer.LookupProtoByMessageName(ctx, namespace, messageName)
		if err != nil {
			return nil, err
		}
		return registeredProto.GetFileDescriptor(), nil
	})
	collectionRepo.SetJSONConverterFactory(converterFactory)
	s.log.Info("Collection repository created with type validation and JSON conversion")

	// ========================================================================
	// 4. Create gRPC Server with ALL Services
	// ========================================================================

	// Audit Logger
	auditLogger := collection.NewAuditLogger(s.systemCollections.Audit)
	s.log.Info("Audit logger initialized")

	// Setup Auth Interceptor (default to no-op if nil)
	authInterceptor := config.AuthInterceptor
	if authInterceptor == nil {
		authInterceptor = security.DefaultAuthInterceptor
	}

	grpcServer := registry.NewServerWithValidation(
		registryServer,
		config.Namespace,
		grpc.ChainUnaryInterceptor(authInterceptor, auditLogger.UnaryServerInterceptor()),
	)
	s.grpcServer = grpcServer

	// Register ALL services on the same server

	// 1. Registry Service
	pb.RegisterCollectorRegistryServer(grpcServer, registryServer)
	s.log.Info("Registered CollectorRegistry service")

	// 2. Collection Service
	collectionServer := collection.NewCollectionServer(collectionRepo)
	pb.RegisterCollectionServiceServer(grpcServer, collectionServer)
	s.log.Info("Registered CollectionService")

	// 3. CollectionRepo Service
	repoGrpcServer := collection.NewGrpcServer(collectionRepo, pathConfig)
	pb.RegisterCollectionRepoServer(grpcServer, repoGrpcServer)
	s.log.Info("Registered CollectionRepo")

	// ========================================================================
	// 5. Setup Listener (but don't start serving yet)
	// ========================================================================

	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", config.Port))
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}
	s.listener = lis

	// Start server in background so we can connect to it for dispatcher setup
	go grpcServer.Serve(lis)
	time.Sleep(100 * time.Millisecond) // Let server start

	actualAddr := lis.Addr().String()
	s.log.Info("Server started", "address", actualAddr)

	// ========================================================================
	// 6. Setup Dispatcher with gRPC-based Registry Validation
	// ========================================================================

	// Create loopback gRPC connection
	loopbackConn, err := grpc.NewClient(actualAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create loopback connection: %w", err)
	}
	s.loopbackConn = loopbackConn

	// Create Registry client for validation via gRPC
	registryClient := pb.NewCollectorRegistryClient(loopbackConn)

	// Wrap the registry client to implement the ServiceMethodValidator interface
	grpcValidator := &grpcRegistryClientValidator{client: registryClient}
	validator := registry.NewGRPCRegistryValidator(grpcValidator)

	// Create dispatcher with gRPC-based validation
	dispatcher := dispatch.NewDispatcherWithRegistry(
		config.CollectorID,
		actualAddr,
		[]string{config.Namespace},
		validator,
		s.systemCollections.Connections,
	)
	s.dispatcher = dispatcher
	s.log.Info("Dispatcher created with gRPC-based registry validation")

	// Register Dispatcher service
	pb.RegisterCollectiveDispatcherServer(grpcServer, dispatcher)
	s.log.Info("Registered CollectiveDispatcher service")

	// Recover connections from previous session
	if err := dispatcher.GetConnectionManager().RecoverFromRestart(ctx); err != nil {
		s.log.Warn("Failed to recover connections", "error", err)
	}

	s.log.Info("Collector started successfully",
		"id", config.CollectorID,
		"port", config.Port,
		"namespace", config.Namespace,
		"validation", "ENABLED",
		"services", "CollectorRegistry, CollectionService, CollectiveDispatcher, CollectionRepo",
	)

	return s, nil
}

// Start starts the server (blocks until shutdown).
// This is a no-op since the server is already started in New().
// Use WaitForShutdown() to block until a shutdown signal is received.
func (s *Server) Start() error {
	// Server is already running from New()
	// This method exists for API compatibility
	return nil
}

// WaitForShutdown blocks until a shutdown signal is received (SIGINT, SIGTERM).
func (s *Server) WaitForShutdown() {
	s.logger.Println("Press Ctrl+C to shutdown")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	s.logger.Println("\nShutting down...")
	s.Shutdown()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() {
		if s.grpcServer != nil {
			s.grpcServer.GracefulStop()
		}
		if s.dispatcher != nil {
			s.dispatcher.Shutdown()
		}
		close(s.shutdownChan)
		s.logger.Println("Shutdown complete")
	})
}

// Close closes all resources. Should be called when the server is no longer needed.
func (s *Server) Close() error {
	s.Shutdown()

	var errors []error

	if s.loopbackConn != nil {
		if err := s.loopbackConn.Close(); err != nil {
			errors = append(errors, fmt.Errorf("close loopback conn: %w", err))
		}
	}

	if s.registryStore != nil {
		if err := s.registryStore.Close(); err != nil {
			errors = append(errors, fmt.Errorf("close registry store: %w", err))
		}
	}

	for i, store := range s.stores {
		if err := store.Close(); err != nil {
			errors = append(errors, fmt.Errorf("close store %d: %w", i, err))
		}
	}

	if s.systemCollections != nil {
		if err := s.systemCollections.Close(); err != nil {
			errors = append(errors, fmt.Errorf("close system collections: %w", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("close errors: %v", errors)
	}

	return nil
}

// Port returns the actual port the server is listening on.
func (s *Server) Port() int {
	if s.listener == nil {
		return 0
	}
	return s.listener.Addr().(*net.TCPAddr).Port
}

// Address returns the full address the server is listening on.
func (s *Server) Address() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// CollectorID returns the unique identifier for this collector.
func (s *Server) CollectorID() string {
	return s.config.CollectorID
}

// Namespace returns the default namespace for this collector.
func (s *Server) Namespace() string {
	return s.config.Namespace
}

// grpcRegistryClientValidator wraps a gRPC Registry client to implement ServiceMethodValidator
type grpcRegistryClientValidator struct {
	client pb.CollectorRegistryClient
}

// ValidateServiceMethod validates by calling the Registry via gRPC
func (v *grpcRegistryClientValidator) ValidateServiceMethod(ctx context.Context, namespace, serviceName, methodName string) error {
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
