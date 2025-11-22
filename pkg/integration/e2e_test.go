package integration

import (
	"context"
	"net"
	"path/filepath"
	"testing"
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
	"google.golang.org/protobuf/types/known/anypb"
)

// TestEndToEndIntegration tests the complete system with registry validation
func TestEndToEndIntegration(t *testing.T) {
	ctx := context.Background()
	namespace := "e2e-test"

	// ========================================================================
	// 1. Setup Registry
	// ========================================================================

	tempDir := t.TempDir()

	// Create registry collections
	protosStore, err := sqlite.NewSqliteStore(
		filepath.Join(tempDir, "protos.db"),
		collection.Options{EnableJSON: true},
	)
	if err != nil {
		t.Fatalf("failed to create protos store: %v", err)
	}
	defer protosStore.Close()

	registeredProtos, err := collection.NewCollection(
		&pb.Collection{Namespace: "system", Name: "registered_protos"},
		protosStore,
		&collection.LocalFileSystem{},
	)
	if err != nil {
		t.Fatalf("failed to create protos collection: %v", err)
	}

	servicesStore, err := sqlite.NewSqliteStore(
		filepath.Join(tempDir, "services.db"),
		collection.Options{EnableJSON: true},
	)
	if err != nil {
		t.Fatalf("failed to create services store: %v", err)
	}
	defer servicesStore.Close()

	registeredServices, err := collection.NewCollection(
		&pb.Collection{Namespace: "system", Name: "registered_services"},
		servicesStore,
		&collection.LocalFileSystem{},
	)
	if err != nil {
		t.Fatalf("failed to create services collection: %v", err)
	}

	registryServer := registry.NewRegistryServer(registeredProtos, registeredServices)

	t.Log("✓ Registry server created (services will be registered when servers start)")

	// ========================================================================
	// 2. Setup CollectionRepo
	// ========================================================================

	repoStore, err := sqlite.NewSqliteStore(
		filepath.Join(tempDir, "repo.db"),
		collection.Options{EnableJSON: true},
	)
	if err != nil {
		t.Fatalf("failed to create repo store: %v", err)
	}
	defer repoStore.Close()

	collectionRepo := collection.NewCollectionRepo(repoStore)

	// ========================================================================
	// 3. Setup Dispatcher with Registry
	// ========================================================================

	validator := registry.NewRegistryValidator(registryServer)
	dispatcher := dispatch.NewDispatcherWithRegistry(
		"test-collector",
		"localhost:0",
		[]string{namespace},
		validator,
	)

	t.Log("✓ Dispatcher created with registry validation")

	// ========================================================================
	// 4. Start Servers
	// ========================================================================

	// Start CollectionService
	collectionGrpcServer, collectionLis, err := registry.SetupCollectionServiceWithValidation(
		ctx,
		registryServer,
		namespace,
		collectionRepo,
		"localhost:0",
	)
	if err != nil {
		t.Fatalf("failed to setup CollectionService: %v", err)
	}
	defer collectionGrpcServer.Stop()

	go collectionGrpcServer.Serve(collectionLis)
	time.Sleep(100 * time.Millisecond) // Let server start

	t.Logf("✓ CollectionService started on %s", collectionLis.Addr())

	// Start Dispatcher
	dispatcherGrpcServer, dispatcherLis, err := registry.SetupDispatcherWithValidation(
		ctx,
		registryServer,
		namespace,
		dispatcher,
		"localhost:0",
	)
	if err != nil {
		t.Fatalf("failed to setup Dispatcher: %v", err)
	}
	defer dispatcherGrpcServer.Stop()

	go dispatcherGrpcServer.Serve(dispatcherLis)
	time.Sleep(100 * time.Millisecond)

	t.Logf("✓ Dispatcher started on %s", dispatcherLis.Addr())

	// Start CollectionRepo
	repoGrpcServer := collection.NewGrpcServer(collectionRepo)
	repoGrpcServerWrapped, repoLis, err := registry.SetupCollectionRepoWithValidation(
		ctx,
		registryServer,
		namespace,
		repoGrpcServer,
		"localhost:0",
	)
	if err != nil {
		t.Fatalf("failed to setup CollectionRepo: %v", err)
	}
	defer repoGrpcServerWrapped.Stop()

	go repoGrpcServerWrapped.Serve(repoLis)
	time.Sleep(100 * time.Millisecond)

	t.Logf("✓ CollectionRepo started on %s", repoLis.Addr())

	// ========================================================================
	// 5. Test Client Calls (with validation)
	// ========================================================================

	// Create client for CollectionService
	collectionConn, err := grpc.NewClient(
		collectionLis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to connect to CollectionService: %v", err)
	}
	defer collectionConn.Close()

	collectionClient := pb.NewCollectionServiceClient(collectionConn)

	// Test CollectionService Meta (registered method)
	t.Run("CollectionService_Meta_Success", func(t *testing.T) {
		_, err := collectionClient.Meta(ctx, &pb.MetaRequest{})
		// May fail with NotFound (no collections), but should NOT fail with Unimplemented
		if err != nil && grpc.Code(err).String() == "Unimplemented" {
			t.Errorf("method was rejected by validation: %v", err)
		}
	})

	// Create client for Dispatcher
	dispatcherConn, err := grpc.NewClient(
		dispatcherLis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to connect to Dispatcher: %v", err)
	}
	defer dispatcherConn.Close()

	dispatcherClient := pb.NewCollectiveDispatcherClient(dispatcherConn)

	// Test Dispatcher Connect (registered method)
	t.Run("Dispatcher_Connect_Success", func(t *testing.T) {
		resp, err := dispatcherClient.Connect(ctx, &pb.ConnectRequest{
			Address:    "test:1234",
			Namespaces: []string{namespace},
		})
		if err != nil {
			t.Errorf("Connect failed: %v", err)
		}
		if resp.Status.Code != 0 {
			t.Logf("Connect response: %+v", resp)
		}
	})

	// Test Dispatcher Serve (registered method, with registry validation)
	t.Run("Dispatcher_Serve_WithValidation", func(t *testing.T) {
		// Register a test service in the dispatcher
		dispatcher.RegisterService(namespace, "TestService", "TestMethod",
			func(ctx context.Context, input interface{}) (interface{}, error) {
				return &anypb.Any{TypeUrl: "test", Value: []byte("success")}, nil
			})

		// Register the test service in the registry too
		_, err := registryServer.RegisterService(ctx, &pb.RegisterServiceRequest{
			Namespace: namespace,
			ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
				Name: proto.String("TestService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{Name: proto.String("TestMethod")},
				},
			},
		})
		if err != nil {
			t.Fatalf("failed to register TestService: %v", err)
		}

		// Call the service - should succeed
		resp, err := dispatcherClient.Serve(ctx, &pb.ServeRequest{
			Namespace:  namespace,
			Service:    &pb.ServiceTypeRef{ServiceName: "TestService"},
			MethodName: "TestMethod",
			Input:      &anypb.Any{},
		})

		if err != nil {
			t.Errorf("Serve failed: %v", err)
		}

		if resp != nil && resp.Status.Code != 200 {
			t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
		}
	})

	// Create client for CollectionRepo
	repoConn, err := grpc.NewClient(
		repoLis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to connect to CollectionRepo: %v", err)
	}
	defer repoConn.Close()

	repoClient := pb.NewCollectionRepoClient(repoConn)

	// Test CollectionRepo CreateCollection (registered method)
	t.Run("CollectionRepo_CreateCollection_Success", func(t *testing.T) {
		resp, err := repoClient.CreateCollection(ctx, &pb.CreateCollectionRequest{
			Collection: &pb.Collection{
				Namespace: "test-ns",
				Name:      "test-collection",
			},
		})

		if err != nil && grpc.Code(err).String() == "Unimplemented" {
			t.Errorf("method was rejected by validation: %v", err)
		}

		if err == nil && resp != nil {
			t.Logf("CreateCollection succeeded: %+v", resp)
		}
	})

	// ========================================================================
	// 6. Test Registry Queries
	// ========================================================================

	t.Run("Registry_ListServices", func(t *testing.T) {
		resp, err := registryServer.ListServices(ctx, &pb.ListServicesRequest{
			Namespace: namespace,
		})
		if err != nil {
			t.Fatalf("failed to list services: %v", err)
		}

		// Should have 4 services: CollectionService, Dispatcher, CollectionRepo, TestService
		if len(resp.Services) != 4 {
			t.Errorf("expected 4 services, got %d", len(resp.Services))
		}

		serviceNames := make(map[string]bool)
		for _, svc := range resp.Services {
			serviceNames[svc.ServiceName] = true
		}

		expected := []string{"CollectionService", "CollectiveDispatcher", "CollectionRepo", "TestService"}
		for _, name := range expected {
			if !serviceNames[name] {
				t.Errorf("service %s not found in registry", name)
			}
		}
	})

	t.Run("Registry_ValidateMethod", func(t *testing.T) {
		// Valid method
		resp, err := registryServer.ValidateMethod(ctx, &pb.ValidateMethodRequest{
			Namespace:   namespace,
			ServiceName: "CollectionService",
			MethodName:  "Create",
		})
		if err != nil {
			t.Errorf("ValidateMethod failed: %v", err)
		}
		if !resp.IsValid {
			t.Errorf("Create method should be valid: %s", resp.Message)
		}

		// Invalid method
		resp, err = registryServer.ValidateMethod(ctx, &pb.ValidateMethodRequest{
			Namespace:   namespace,
			ServiceName: "CollectionService",
			MethodName:  "NonexistentMethod",
		})
		if err != nil {
			t.Errorf("ValidateMethod failed: %v", err)
		}
		if resp.IsValid {
			t.Error("NonexistentMethod should be invalid")
		}
	})

	t.Log("✓ All integration tests passed!")
}

// TestBackwardsCompatibility ensures old code without registry still works
func TestBackwardsCompatibility(t *testing.T) {
	ctx := context.Background()
	namespace := "compat-test"

	// Create dispatcher WITHOUT registry (old way)
	dispatcher := dispatch.NewDispatcher(
		"old-dispatcher",
		"localhost:0",
		[]string{namespace},
	)

	// Register a service
	dispatcher.RegisterService(namespace, "TestService", "TestMethod",
		func(ctx context.Context, input interface{}) (interface{}, error) {
			return &anypb.Any{TypeUrl: "test", Value: []byte("success")}, nil
		})

	// Start server WITHOUT validation
	grpcServer := grpc.NewServer()
	pb.RegisterCollectiveDispatcherServer(grpcServer, dispatcher)

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	go grpcServer.Serve(lis)
	defer grpcServer.Stop()

	time.Sleep(100 * time.Millisecond)

	// Create client
	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)

	// Test: should work without registry
	t.Run("NoRegistry_StillWorks", func(t *testing.T) {
		resp, err := client.Serve(ctx, &pb.ServeRequest{
			Namespace:  namespace,
			Service:    &pb.ServiceTypeRef{ServiceName: "TestService"},
			MethodName: "TestMethod",
			Input:      &anypb.Any{},
		})

		if err != nil {
			t.Errorf("Serve failed: %v", err)
		}

		if resp.Status.Code != 200 {
			t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
		}

		t.Log("✓ Old code without registry still works")
	})
}
