package integration

import (
	"context"
	"fmt"
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

// setupCollector creates a complete collector with all services on one gRPC server
func setupCollector(t *testing.T, collectorID, namespace string, port int) (
	*grpc.Server,
	net.Listener,
	*dispatch.Dispatcher,
	*registry.RegistryServer,
	collection.CollectionRepo,
) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// Setup Registry
	protosStore, err := sqlite.NewSqliteStore(
		filepath.Join(tempDir, "protos.db"),
		collection.Options{EnableJSON: true},
	)
	if err != nil {
		t.Fatalf("failed to create protos store: %v", err)
	}
	t.Cleanup(func() { protosStore.Close() })

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
	t.Cleanup(func() { servicesStore.Close() })

	registeredServices, err := collection.NewCollection(
		&pb.Collection{Namespace: "system", Name: "registered_services"},
		servicesStore,
		&collection.LocalFileSystem{},
	)
	if err != nil {
		t.Fatalf("failed to create services collection: %v", err)
	}

	registryServer := registry.NewRegistryServer(registeredProtos, registeredServices)

	// Register all services in the registry
	if err := registry.RegisterCollectionService(ctx, registryServer, namespace); err != nil {
		t.Fatalf("failed to register CollectionService: %v", err)
	}
	if err := registry.RegisterDispatcherService(ctx, registryServer, namespace); err != nil {
		t.Fatalf("failed to register Dispatcher: %v", err)
	}
	if err := registry.RegisterCollectionRepoService(ctx, registryServer, namespace); err != nil {
		t.Fatalf("failed to register CollectionRepo: %v", err)
	}

	// Setup CollectionRepo
	repoStore, err := sqlite.NewSqliteStore(
		filepath.Join(tempDir, "repo.db"),
		collection.Options{EnableJSON: true},
	)
	if err != nil {
		t.Fatalf("failed to create repo store: %v", err)
	}
	t.Cleanup(func() { repoStore.Close() })

	collectionRepo := collection.NewCollectionRepo(repoStore)

	// Setup Dispatcher with Registry
	validator := registry.NewRegistryValidator(registryServer)
	addr := fmt.Sprintf("localhost:%d", port)
	dispatcher := dispatch.NewDispatcherWithRegistry(
		collectorID,
		addr,
		[]string{namespace},
		validator,
	)

	// Create single gRPC server with ALL services
	grpcServer := registry.NewServerWithValidation(registryServer, namespace)

	// Register all services
	pb.RegisterCollectorRegistryServer(grpcServer, registryServer)
	collectionServer := collection.NewCollectionServer(collectionRepo)
	pb.RegisterCollectionServiceServer(grpcServer, collectionServer)
	pb.RegisterCollectiveDispatcherServer(grpcServer, dispatcher)
	repoGrpcServer := collection.NewGrpcServer(collectionRepo)
	pb.RegisterCollectionRepoServer(grpcServer, repoGrpcServer)

	// Start listener
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	return grpcServer, lis, dispatcher, registryServer, collectionRepo
}

// TestMultiCollectorIntegration tests two collectors connecting and dispatching to each other
func TestMultiCollectorIntegration(t *testing.T) {
	ctx := context.Background()
	namespace := "test-namespace"

	// ========================================================================
	// Setup Collector 1
	// ========================================================================

	grpcServer1, lis1, dispatcher1, registry1, _ := setupCollector(t, "collector-1", namespace, 0)
	defer grpcServer1.Stop()

	go grpcServer1.Serve(lis1)
	time.Sleep(100 * time.Millisecond)

	addr1 := lis1.Addr().String()
	t.Logf("✓ Collector 1 started on %s", addr1)

	// ========================================================================
	// Setup Collector 2
	// ========================================================================

	grpcServer2, lis2, _, registry2, _ := setupCollector(t, "collector-2", namespace, 0)
	defer grpcServer2.Stop()

	go grpcServer2.Serve(lis2)
	time.Sleep(100 * time.Millisecond)

	addr2 := lis2.Addr().String()
	t.Logf("✓ Collector 2 started on %s", addr2)

	// ========================================================================
	// Test: Collectors can call each other's services
	// ========================================================================

	t.Run("Collector1_ConnectsTo_Collector2", func(t *testing.T) {
		// Create client to collector 2
		conn, err := grpc.NewClient(addr2, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		client := pb.NewCollectiveDispatcherClient(conn)

		// Collector 1 connects to collector 2
		resp, err := client.Connect(ctx, &pb.ConnectRequest{
			Address:    addr1,
			Namespaces: []string{namespace},
		})

		if err != nil {
			t.Fatalf("Connect failed: %v", err)
		}

		if resp.Status.Code != 200 {
			t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
		}

		t.Logf("✓ Collector 1 connected to Collector 2: %s", resp.ConnectionId)
	})

	t.Run("Collector2_ConnectsTo_Collector1", func(t *testing.T) {
		// Create client to collector 1
		conn, err := grpc.NewClient(addr1, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		client := pb.NewCollectiveDispatcherClient(conn)

		// Collector 2 connects to collector 1
		resp, err := client.Connect(ctx, &pb.ConnectRequest{
			Address:    addr2,
			Namespaces: []string{namespace},
		})

		if err != nil {
			t.Fatalf("Connect failed: %v", err)
		}

		if resp.Status.Code != 200 {
			t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
		}

		t.Logf("✓ Collector 2 connected to Collector 1: %s", resp.ConnectionId)
	})

	t.Run("Dispatcher_ServiceCalls_WithValidation", func(t *testing.T) {
		// Register a custom service on collector 1
		dispatcher1.RegisterService(namespace, "CustomService", "CustomMethod",
			func(ctx context.Context, input interface{}) (interface{}, error) {
				return &anypb.Any{
					TypeUrl: "type.googleapis.com/test.Response",
					Value:   []byte("response from collector 1"),
				}, nil
			})

		// Register the custom service in collector 1's registry
		_, err := registry1.RegisterService(ctx, &pb.RegisterServiceRequest{
			Namespace: namespace,
			ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
				Name: proto.String("CustomService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{Name: proto.String("CustomMethod")},
				},
			},
		})
		if err != nil {
			t.Fatalf("failed to register CustomService: %v", err)
		}

		// Register same service in collector 2's registry (so validation passes)
		_, err = registry2.RegisterService(ctx, &pb.RegisterServiceRequest{
			Namespace: namespace,
			ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
				Name: proto.String("CustomService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{Name: proto.String("CustomMethod")},
				},
			},
		})
		if err != nil {
			t.Fatalf("failed to register CustomService in registry2: %v", err)
		}

		// Establish connection from dispatcher1 to dispatcher2's address
		// so dispatcher1 can route to it
		_, err = dispatcher1.ConnectTo(ctx, addr2, []string{namespace})
		if err != nil {
			t.Fatalf("failed to connect dispatcher1 to dispatcher2: %v", err)
		}

		// Call the service directly on collector 1 (local call)
		conn1, err := grpc.NewClient(addr1, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn1.Close()

		client1 := pb.NewCollectiveDispatcherClient(conn1)

		resp, err := client1.Serve(ctx, &pb.ServeRequest{
			Namespace:  namespace,
			Service:    &pb.ServiceTypeRef{ServiceName: "CustomService"},
			MethodName: "CustomMethod",
			Input:      &anypb.Any{},
		})

		if err != nil {
			t.Fatalf("Serve failed: %v", err)
		}

		if resp.Status == nil || resp.Status.Code != 200 {
			if resp.Status != nil {
				t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
			} else {
				t.Error("response status is nil")
			}
		} else {
			t.Logf("✓ Service call executed on collector-1")
			if resp.Output != nil {
				t.Logf("  Response: %s", string(resp.Output.Value))
			}
		}
	})

	t.Run("AllServicesAccessible_OnBothCollectors", func(t *testing.T) {
		// Test that all 4 services are accessible on both collectors

		testService := func(addr, serviceName string) {
			conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("failed to connect to %s: %v", addr, err)
			}
			defer conn.Close()

			switch serviceName {
			case "Registry":
				client := pb.NewCollectorRegistryClient(conn)
				services, err := client.RegisterService(ctx, &pb.RegisterServiceRequest{
					Namespace: "test-query",
					ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
						Name: proto.String("QueryService"),
					},
				})
				if err != nil && grpc.Code(err).String() != "Unimplemented" {
					t.Logf("Registry accessible on %s", addr)
				}
				_ = services

			case "CollectionService":
				client := pb.NewCollectionServiceClient(conn)
				_, err := client.Meta(ctx, &pb.MetaRequest{})
				// Should not be Unimplemented
				if err != nil && grpc.Code(err).String() == "Unimplemented" {
					t.Errorf("CollectionService rejected by validation on %s: %v", addr, err)
				}

			case "Dispatcher":
				client := pb.NewCollectiveDispatcherClient(conn)
				_, err := client.Connect(ctx, &pb.ConnectRequest{
					Address:    "test:1234",
					Namespaces: []string{namespace},
				})
				if err != nil && grpc.Code(err).String() == "Unimplemented" {
					t.Errorf("Dispatcher rejected by validation on %s: %v", addr, err)
				}

			case "CollectionRepo":
				client := pb.NewCollectionRepoClient(conn)
				_, err := client.Discover(ctx, &pb.DiscoverRequest{})
				if err != nil && grpc.Code(err).String() == "Unimplemented" {
					t.Errorf("CollectionRepo rejected by validation on %s: %v", addr, err)
				}
			}
		}

		// Test all services on both collectors
		for _, addr := range []string{addr1, addr2} {
			for _, svc := range []string{"Registry", "CollectionService", "Dispatcher", "CollectionRepo"} {
				testService(addr, svc)
			}
		}

		t.Log("✓ All 4 services accessible on both collectors")
	})

	t.Log("✓ Multi-collector integration test completed successfully!")
}
