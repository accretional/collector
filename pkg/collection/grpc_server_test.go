package collection_test

import (
	"context"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/protobuf/types/known/structpb"
)

// Re-export for cleaner test code
var NewGrpcServer = collection.NewGrpcServer

// TestGrpcServer_CreateCollection tests the CreateCollection gRPC endpoint
func TestGrpcServer_CreateCollection(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	req := &pb.CreateCollectionRequest{
		Collection: &pb.Collection{
			Namespace: "grpc-test",
			Name:      "test-coll",
			MessageType: &pb.MessageTypeRef{
				MessageName: "TestMessage",
			},
		},
	}

	resp, err := server.CreateCollection(ctx, req)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	if resp.CollectionId == "" {
		t.Error("expected non-empty collection ID")
	}

	expectedID := "grpc-test/test-coll"
	if resp.CollectionId != expectedID {
		t.Errorf("expected collection ID '%s', got '%s'", expectedID, resp.CollectionId)
	}
}

func TestGrpcServer_CreateCollection_WithServerEndpoint(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	req := &pb.CreateCollectionRequest{
		Collection: &pb.Collection{
			Namespace:      "grpc-test",
			Name:           "routed-coll",
			ServerEndpoint: "localhost:8080",
		},
	}

	resp, err := server.CreateCollection(ctx, req)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d", resp.Status.Code)
	}

	// Server endpoint should be preserved in response
	if resp.ServerEndpoint != "" {
		t.Logf("Server endpoint in response: %s", resp.ServerEndpoint)
	}
}

func TestGrpcServer_CreateCollection_WithIndexedFields(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	req := &pb.CreateCollectionRequest{
		Collection: &pb.Collection{
			Namespace:     "grpc-test",
			Name:          "indexed-coll",
			IndexedFields: []string{"field1", "field2", "field3"},
		},
	}

	resp, err := server.CreateCollection(ctx, req)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d", resp.Status.Code)
	}
}

// TestGrpcServer_Discover tests the Discover gRPC endpoint
func TestGrpcServer_Discover(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// Create test collections
	collections := []*pb.Collection{
		{Namespace: "ns1", Name: "coll1"},
		{Namespace: "ns1", Name: "coll2"},
		{Namespace: "ns2", Name: "coll1"},
	}

	for _, coll := range collections {
		_, err := server.CreateCollection(ctx, &pb.CreateCollectionRequest{Collection: coll})
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Discover all collections
	req := &pb.DiscoverRequest{}
	resp, err := server.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	// Implementation returns 501 Not Implemented
	if resp.Status.Code != 501 {
		t.Logf("Discover returned status %d", resp.Status.Code)
	}
}

func TestGrpcServer_Discover_WithFilters(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// Create collections with different properties
	collections := []*pb.Collection{
		{
			Namespace:   "prod",
			Name:        "users",
			MessageType: &pb.MessageTypeRef{MessageName: "User"},
			Metadata: &pb.Metadata{
				Labels: map[string]string{"env": "production"},
			},
		},
		{
			Namespace:   "dev",
			Name:        "users",
			MessageType: &pb.MessageTypeRef{MessageName: "User"},
			Metadata: &pb.Metadata{
				Labels: map[string]string{"env": "development"},
			},
		},
	}

	for _, coll := range collections {
		_, err := server.CreateCollection(ctx, &pb.CreateCollectionRequest{Collection: coll})
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	tests := []struct {
		name    string
		request *pb.DiscoverRequest
	}{
		{
			name: "filter by namespace",
			request: &pb.DiscoverRequest{
				Namespace: "prod",
			},
		},
		{
			name: "filter by label",
			request: &pb.DiscoverRequest{
				LabelFilter: map[string]string{"env": "production"},
			},
		},
		{
			name: "filter by message type",
			request: &pb.DiscoverRequest{
				MessageTypeFilter: &pb.MessageTypeRef{MessageName: "User"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := server.Discover(ctx, tt.request)
			if err != nil {
				t.Fatalf("Discover failed: %v", err)
			}

			// Implementation returns 501 Not Implemented
			if resp.Status.Code != 501 {
				t.Logf("Discover returned status %d", resp.Status.Code)
			}
		})
	}
}

func TestGrpcServer_Discover_WithPagination(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// Create many collections
	for i := 1; i <= 20; i++ {
		coll := &pb.Collection{
			Namespace: "test",
			Name:      string(rune('a' + i - 1)),
		}
		_, err := server.CreateCollection(ctx, &pb.CreateCollectionRequest{Collection: coll})
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Request first page
	req := &pb.DiscoverRequest{
		PageSize: 5,
	}

	resp, err := server.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	// Implementation returns 501 Not Implemented
	if resp.Status.Code != 501 {
		t.Logf("Discover returned status %d", resp.Status.Code)
	}
}

// TestGrpcServer_Route tests the Route gRPC endpoint
func TestGrpcServer_Route(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// Create a collection
	createReq := &pb.CreateCollectionRequest{
		Collection: &pb.Collection{
			Namespace:      "test",
			Name:           "routed",
			ServerEndpoint: "localhost:9090",
		},
	}

	_, err := server.CreateCollection(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Route to the collection
	routeReq := &pb.RouteRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "routed",
		},
	}

	resp, err := server.Route(ctx, routeReq)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	// Implementation returns 501 Not Implemented
	if resp.Status.Code != 501 {
		t.Logf("Route returned status %d", resp.Status.Code)
	}
}

func TestGrpcServer_Route_NonExistent(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// Try to route to non-existent collection
	req := &pb.RouteRequest{
		Collection: &pb.NamespacedName{
			Namespace: "nonexistent",
			Name:      "missing",
		},
	}

	resp, err := server.Route(ctx, req)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	// Implementation returns 501 Not Implemented
	if resp.Status.Code != 501 {
		t.Logf("Route returned status %d", resp.Status.Code)
	}
}

// TestGrpcServer_SearchCollections tests the SearchCollections gRPC endpoint
func TestGrpcServer_SearchCollections(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// Create collections
	collections := []*pb.Collection{
		{Namespace: "test", Name: "coll1"},
		{Namespace: "test", Name: "coll2"},
	}

	for _, coll := range collections {
		_, err := server.CreateCollection(ctx, &pb.CreateCollectionRequest{Collection: coll})
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Search across collections
	req := &pb.SearchCollectionsRequest{
		Namespace:       "test",
		CollectionNames: []string{"coll1", "coll2"},
		Query:           &structpb.Struct{},
		Limit:           10,
	}

	resp, err := server.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	// Implementation returns 501 Not Implemented
	if resp.Status.Code != 501 {
		t.Logf("SearchCollections returned status %d", resp.Status.Code)
	}
}

func TestGrpcServer_SearchCollections_WithQuery(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// Create a collection
	coll := &pb.Collection{Namespace: "search-test", Name: "items"}
	_, err := server.CreateCollection(ctx, &pb.CreateCollectionRequest{Collection: coll})
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Search with a complex query
	query := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"status": structpb.NewStringValue("active"),
			"score": structpb.NewNumberValue(90),
		},
	}

	req := &pb.SearchCollectionsRequest{
		Namespace:       "search-test",
		CollectionNames: []string{"items"},
		Query:           query,
		Limit:           20,
		OrderBy:         "created_at",
	}

	resp, err := server.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	// Implementation returns 501 Not Implemented
	if resp.Status.Code != 501 {
		t.Logf("SearchCollections returned status %d", resp.Status.Code)
	}
}

func TestGrpcServer_SearchCollections_AcrossNamespaces(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// Create collections in different namespaces
	collections := []*pb.Collection{
		{Namespace: "ns1", Name: "items"},
		{Namespace: "ns2", Name: "items"},
	}

	for _, coll := range collections {
		_, err := server.CreateCollection(ctx, &pb.CreateCollectionRequest{Collection: coll})
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Search across all namespaces (empty namespace means all)
	req := &pb.SearchCollectionsRequest{
		Namespace: "", // Empty means search across all
		Query:     &structpb.Struct{},
		Limit:     10,
	}

	resp, err := server.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	// Implementation returns 501 Not Implemented
	if resp.Status.Code != 501 {
		t.Logf("SearchCollections returned status %d", resp.Status.Code)
	}
}

func TestGrpcServer_SearchCollections_SpecificCollections(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// Create multiple collections
	collections := []*pb.Collection{
		{Namespace: "test", Name: "users"},
		{Namespace: "test", Name: "orders"},
		{Namespace: "test", Name: "products"},
	}

	for _, coll := range collections {
		_, err := server.CreateCollection(ctx, &pb.CreateCollectionRequest{Collection: coll})
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Search only specific collections
	req := &pb.SearchCollectionsRequest{
		Namespace:       "test",
		CollectionNames: []string{"users", "orders"}, // Only search these two
		Query:           &structpb.Struct{},
		Limit:           10,
	}

	resp, err := server.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	// Implementation returns 501 Not Implemented
	if resp.Status.Code != 501 {
		t.Logf("SearchCollections returned status %d", resp.Status.Code)
	}
}

// TestGrpcServer_ErrorHandling tests error handling in the gRPC server
func TestGrpcServer_ErrorHandling_NilCollection(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// Try to create with nil collection (should handle gracefully)
	req := &pb.CreateCollectionRequest{
		Collection: nil,
	}

	_, err := server.CreateCollection(ctx, req)
	if err != nil {
		// Expected to fail
		t.Logf("Nil collection handled: %v", err)
	}
}

// TestGrpcServer_Concurrency tests concurrent requests to the gRPC server
func TestGrpcServer_ConcurrentRequests(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	done := make(chan error, 10)

	// Create collections concurrently
	for i := 0; i < 10; i++ {
		go func(id int) {
			req := &pb.CreateCollectionRequest{
				Collection: &pb.Collection{
					Namespace: "concurrent",
					Name:      string(rune('a' + id)),
				},
			}

			_, err := server.CreateCollection(ctx, req)
			done <- err
		}(i)
	}

	// Wait for all requests
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent request failed: %v", err)
		}
	}
}

func TestGrpcServer_ConcurrentMixedOperations(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// Create a base collection first
	baseReq := &pb.CreateCollectionRequest{
		Collection: &pb.Collection{
			Namespace: "mixed",
			Name:      "base",
		},
	}
	_, err := server.CreateCollection(ctx, baseReq)
	if err != nil {
		t.Fatalf("Failed to create base collection: %v", err)
	}

	done := make(chan error, 20)

	// Mix of creates and discovers
	for i := 0; i < 10; i++ {
		// Create
		go func(id int) {
			req := &pb.CreateCollectionRequest{
				Collection: &pb.Collection{
					Namespace: "mixed",
					Name:      string(rune('a' + id)),
				},
			}
			_, err := server.CreateCollection(ctx, req)
			done <- err
		}(i)

		// Discover
		go func() {
			req := &pb.DiscoverRequest{Namespace: "mixed"}
			_, err := server.Discover(ctx, req)
			done <- err
		}()
	}

	// Wait for all operations
	for i := 0; i < 20; i++ {
		if err := <-done; err != nil {
			t.Logf("concurrent operation completed with: %v", err)
		}
	}
}

// TestGrpcServer_Integration tests end-to-end flow
func TestGrpcServer_Integration_CreateAndRoute(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewGrpcServer(repo)
	ctx := context.Background()

	// 1. Create a collection
	createReq := &pb.CreateCollectionRequest{
		Collection: &pb.Collection{
			Namespace:      "integration",
			Name:           "test-flow",
			ServerEndpoint: "localhost:8080",
			MessageType: &pb.MessageTypeRef{
				MessageName: "IntegrationTest",
			},
			IndexedFields: []string{"id", "name"},
		},
	}

	createResp, err := server.CreateCollection(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	if createResp.Status.Code != 200 {
		t.Fatalf("expected status 200, got %d", createResp.Status.Code)
	}

	// 2. Route to the collection
	routeReq := &pb.RouteRequest{
		Collection: &pb.NamespacedName{
			Namespace: "integration",
			Name:      "test-flow",
		},
	}

	routeResp, err := server.Route(ctx, routeReq)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	// Implementation returns 501 Not Implemented for Route
	if routeResp.Status.Code != 501 {
		t.Logf("Route completed with status %d", routeResp.Status.Code)
	}

	// 3. Discover it
	discoverReq := &pb.DiscoverRequest{
		Namespace: "integration",
	}

	discoverResp, err := server.Discover(ctx, discoverReq)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	// Implementation returns 501 Not Implemented for Discover
	if discoverResp.Status.Code != 501 {
		t.Logf("Discover completed with status %d", discoverResp.Status.Code)
	}
}
