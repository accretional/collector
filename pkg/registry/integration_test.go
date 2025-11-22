package registry

import (
	"context"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// TestCollectionServiceIntegration tests the full integration of registry validation
// with the CollectionService
func TestCollectionServiceIntegration(t *testing.T) {
	ctx := context.Background()

	// 1. Set up registry
	registry, _, _ := setupTestServer(t)

	// 2. Create a mock collection repo
	mockRepo := &mockCollectionRepo{}

	// 3. Set up server with validation (this will register the service)
	grpcServer, lis, err := SetupCollectionServiceWithValidation(
		ctx,
		registry,
		"test",
		mockRepo,
		"localhost:0",
	)
	if err != nil {
		t.Fatalf("failed to set up server: %v", err)
	}
	defer grpcServer.Stop()

	// 5. Start server in background
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			t.Logf("server error: %v", err)
		}
	}()

	// 6. Create client
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectionServiceClient(conn)

	// 7. Test registered method (should work)
	t.Run("RegisteredMethod", func(t *testing.T) {
		_, err := client.Meta(ctx, &pb.MetaRequest{})
		// The method will fail because mockRepo doesn't implement it properly,
		// but we're testing that validation passes
		if err != nil {
			// Check if it's a validation error (which we don't want)
			if status.Code(err) == codes.Unimplemented {
				t.Errorf("method was rejected by validation: %v", err)
			}
			// Other errors are expected from the mock
		}
	})

	// Note: Testing unregistered methods is tricky because all CollectionService
	// methods are registered. We'd need to call a method that doesn't exist,
	// which would be caught by gRPC itself before reaching our interceptor.
}

// TestDispatcherIntegration tests dispatcher service registration and validation
func TestDispatcherIntegration(t *testing.T) {
	ctx := context.Background()

	// 1. Set up registry
	registry, _, _ := setupTestServer(t)

	// 2. Register CollectiveDispatcher in namespace "test"
	err := RegisterDispatcherService(ctx, registry, "test")
	if err != nil {
		t.Fatalf("failed to register Dispatcher: %v", err)
	}

	// 3. Verify all methods are registered
	lookupResp, err := registry.LookupService(ctx, &pb.LookupServiceRequest{
		Namespace:   "test",
		ServiceName: "CollectiveDispatcher",
	})
	if err != nil {
		t.Fatalf("failed to lookup service: %v", err)
	}
	if lookupResp.Status.Code != pb.Status_OK {
		t.Fatalf("LookupService failed: %s", lookupResp.Status.Message)
	}
	service := lookupResp.Service

	expectedMethods := []string{"Serve", "Connect", "Dispatch"}
	if len(service.MethodNames) != len(expectedMethods) {
		t.Errorf("expected %d methods, got %d", len(expectedMethods), len(service.MethodNames))
	}

	for _, method := range expectedMethods {
		found := false
		for _, registered := range service.MethodNames {
			if registered == method {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("method %s not found in registered methods", method)
		}
	}
}

// TestCollectionRepoIntegration tests collection repo service registration and validation
func TestCollectionRepoIntegration(t *testing.T) {
	ctx := context.Background()

	// 1. Set up registry
	registry, _, _ := setupTestServer(t)

	// 2. Register CollectionRepo in namespace "test"
	err := RegisterCollectionRepoService(ctx, registry, "test")
	if err != nil {
		t.Fatalf("failed to register CollectionRepo: %v", err)
	}

	// 3. Verify all methods are registered
	lookupResp, err := registry.LookupService(ctx, &pb.LookupServiceRequest{
		Namespace:   "test",
		ServiceName: "CollectionRepo",
	})
	if err != nil {
		t.Fatalf("failed to lookup service: %v", err)
	}
	if lookupResp.Status.Code != pb.Status_OK {
		t.Fatalf("LookupService failed: %s", lookupResp.Status.Message)
	}
	service := lookupResp.Service

	expectedMethods := []string{"CreateCollection", "Discover", "Route", "SearchCollections"}
	if len(service.MethodNames) != len(expectedMethods) {
		t.Errorf("expected %d methods, got %d", len(expectedMethods), len(service.MethodNames))
	}

	for _, method := range expectedMethods {
		found := false
		for _, registered := range service.MethodNames {
			if registered == method {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("method %s not found in registered methods", method)
		}
	}
}

// TestMultipleNamespaces tests that services can be registered in different namespaces
func TestMultipleNamespaces(t *testing.T) {
	ctx := context.Background()
	registry, _, _ := setupTestServer(t)

	// Register same service in different namespaces
	namespaces := []string{"namespace1", "namespace2", "namespace3"}

	for _, ns := range namespaces {
		err := RegisterCollectionService(ctx, registry, ns)
		if err != nil {
			t.Errorf("failed to register CollectionService in namespace %s: %v", ns, err)
		}
	}

	// Verify all are registered independently
	for _, ns := range namespaces {
		lookupResp, err := registry.LookupService(ctx, &pb.LookupServiceRequest{
			Namespace:   ns,
			ServiceName: "CollectionService",
		})
		if err != nil {
			t.Errorf("failed to lookup service in namespace %s: %v", ns, err)
			continue
		}
		if lookupResp.Status.Code != pb.Status_OK {
			t.Errorf("LookupService failed for namespace %s: %s", ns, lookupResp.Status.Message)
			continue
		}
		service := lookupResp.Service
		if service.Namespace != ns {
			t.Errorf("expected namespace %s, got %s", ns, service.Namespace)
		}
	}
}

// TestDynamicServiceLookup tests dynamic service discovery and method validation
func TestDynamicServiceLookup(t *testing.T) {
	ctx := context.Background()
	registry, _, _ := setupTestServer(t)

	// Register multiple services
	services := []struct {
		registerFunc func(context.Context, *RegistryServer, string) error
		serviceName  string
		methodCount  int
	}{
		{RegisterCollectionService, "CollectionService", 11},
		{RegisterDispatcherService, "CollectiveDispatcher", 3},
		{RegisterCollectionRepoService, "CollectionRepo", 4},
	}

	namespace := "dynamic"
	for _, svc := range services {
		err := svc.registerFunc(ctx, registry, namespace)
		if err != nil {
			t.Errorf("failed to register %s: %v", svc.serviceName, err)
		}
	}

	// List all services in namespace
	listResp, err := registry.ListServices(ctx, &pb.ListServicesRequest{Namespace: namespace})
	if err != nil {
		t.Fatalf("failed to list services: %v", err)
	}
	if listResp.Status.Code != pb.Status_OK {
		t.Fatalf("ListServices failed: %s", listResp.Status.Message)
	}
	allServices := listResp.Services

	if len(allServices) != len(services) {
		t.Errorf("expected %d services, got %d", len(services), len(allServices))
	}

	// Verify each service has correct method count
	for _, svc := range services {
		lookupResp, err := registry.LookupService(ctx, &pb.LookupServiceRequest{
			Namespace:   namespace,
			ServiceName: svc.serviceName,
		})
		if err != nil {
			t.Errorf("failed to lookup %s: %v", svc.serviceName, err)
			continue
		}
		if lookupResp.Status.Code != pb.Status_OK {
			t.Errorf("LookupService failed for %s: %s", svc.serviceName, lookupResp.Status.Message)
			continue
		}

		registeredSvc := lookupResp.Service
		if len(registeredSvc.MethodNames) != svc.methodCount {
			t.Errorf("expected %d methods for %s, got %d",
				svc.methodCount, svc.serviceName, len(registeredSvc.MethodNames))
		}
	}
}

// Mock CollectionRepo for testing
type mockCollectionRepo struct {
	collection.CollectionRepo
}

func (m *mockCollectionRepo) GetCollection(ctx context.Context, namespace, name string) (*collection.Collection, error) {
	return nil, status.Error(codes.NotFound, "mock repo")
}
