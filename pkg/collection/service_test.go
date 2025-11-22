package collection_test

import (
	"context"
	"strings"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
)

// setupTestService creates a test service instance
func setupTestService(t *testing.T) (*collection.CollectionRepoService, func()) {
	t.Helper()

	// Create temp store
	store, err := sqlite.NewSqliteStore(":memory:", collection.Options{
		EnableFTS:  true,
		EnableJSON: true,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	service := collection.NewCollectionRepoService(store)

	cleanup := func() {
		store.Close()
	}

	return service, cleanup
}

// TestService_CreateCollection tests creating collections
func TestService_CreateCollection(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	coll := &pb.Collection{
		Namespace: "service-test",
		Name:      "users",
		MessageType: &pb.MessageTypeRef{
			MessageName: "User",
		},
	}

	resp, err := service.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	expectedID := "service-test/users"
	if resp.CollectionId != expectedID {
		t.Errorf("expected collection ID '%s', got '%s'", expectedID, resp.CollectionId)
	}
}

func TestService_CreateCollection_Duplicate(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	coll := &pb.Collection{
		Namespace: "test",
		Name:      "duplicate",
	}

	// Create first time
	_, err := service.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("first CreateCollection failed: %v", err)
	}

	// Try to create again
	_, err = service.CreateCollection(ctx, coll)
	if err == nil {
		t.Fatal("expected error for duplicate collection")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestService_CreateCollection_Nil(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	_, err := service.CreateCollection(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil collection")
	}

	if !strings.Contains(err.Error(), "cannot be nil") {
		t.Errorf("expected 'cannot be nil' error, got: %v", err)
	}
}

// TestService_Discover tests collection discovery
func TestService_Discover(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// Create test collections
	collections := []*pb.Collection{
		{Namespace: "prod", Name: "users"},
		{Namespace: "prod", Name: "orders"},
		{Namespace: "dev", Name: "users"},
	}

	for _, coll := range collections {
		_, err := service.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Discover all collections
	req := &pb.DiscoverRequest{}
	resp, err := service.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d", resp.Status.Code)
	}

	if len(resp.Collections) != 3 {
		t.Errorf("expected 3 collections, got %d", len(resp.Collections))
	}
}

func TestService_Discover_WithNamespaceFilter(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// Create test collections
	collections := []*pb.Collection{
		{Namespace: "prod", Name: "users"},
		{Namespace: "prod", Name: "orders"},
		{Namespace: "dev", Name: "users"},
	}

	for _, coll := range collections {
		_, err := service.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Discover collections in 'prod' namespace
	req := &pb.DiscoverRequest{
		Namespace: "prod",
	}
	resp, err := service.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(resp.Collections) != 2 {
		t.Errorf("expected 2 collections in 'prod', got %d", len(resp.Collections))
	}

	for _, coll := range resp.Collections {
		if coll.Namespace != "prod" {
			t.Errorf("expected namespace 'prod', got '%s'", coll.Namespace)
		}
	}
}

func TestService_Discover_WithMessageTypeFilter(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// Create collections with different message types
	collections := []*pb.Collection{
		{
			Namespace:   "test",
			Name:        "users",
			MessageType: &pb.MessageTypeRef{MessageName: "User"},
		},
		{
			Namespace:   "test",
			Name:        "orders",
			MessageType: &pb.MessageTypeRef{MessageName: "Order"},
		},
		{
			Namespace:   "test",
			Name:        "products",
			MessageType: &pb.MessageTypeRef{MessageName: "User"},
		},
	}

	for _, coll := range collections {
		_, err := service.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Discover collections with User message type
	req := &pb.DiscoverRequest{
		MessageTypeFilter: &pb.MessageTypeRef{MessageName: "User"},
	}
	resp, err := service.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(resp.Collections) != 2 {
		t.Errorf("expected 2 collections with User type, got %d", len(resp.Collections))
	}
}

func TestService_Discover_WithLabelFilter(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// Create collections with labels
	collections := []*pb.Collection{
		{
			Namespace: "test",
			Name:      "coll1",
			Metadata: &pb.Metadata{
				Labels: map[string]string{"env": "prod", "region": "us-east"},
			},
		},
		{
			Namespace: "test",
			Name:      "coll2",
			Metadata: &pb.Metadata{
				Labels: map[string]string{"env": "dev", "region": "us-west"},
			},
		},
		{
			Namespace: "test",
			Name:      "coll3",
			Metadata: &pb.Metadata{
				Labels: map[string]string{"env": "prod", "region": "us-west"},
			},
		},
	}

	for _, coll := range collections {
		_, err := service.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Discover collections with env=prod
	req := &pb.DiscoverRequest{
		LabelFilter: map[string]string{"env": "prod"},
	}
	resp, err := service.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(resp.Collections) != 2 {
		t.Errorf("expected 2 collections with env=prod, got %d", len(resp.Collections))
	}

	// Discover with multiple label filters
	req = &pb.DiscoverRequest{
		LabelFilter: map[string]string{"env": "prod", "region": "us-west"},
	}
	resp, err = service.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(resp.Collections) != 1 {
		t.Errorf("expected 1 collection with both labels, got %d", len(resp.Collections))
	}
}

func TestService_Discover_Pagination(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// Create many collections
	for i := 0; i < 10; i++ {
		coll := &pb.Collection{
			Namespace: "test",
			Name:      string(rune('a' + i)),
		}
		_, err := service.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Get first page
	req := &pb.DiscoverRequest{
		PageSize: 3,
	}
	page1, err := service.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(page1.Collections) != 3 {
		t.Errorf("expected 3 collections in page 1, got %d", len(page1.Collections))
	}

	if page1.NextPageToken == "" {
		t.Error("expected next page token")
	}

	// Get second page
	req.PageToken = page1.NextPageToken
	page2, err := service.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover page 2 failed: %v", err)
	}

	if len(page2.Collections) != 3 {
		t.Errorf("expected 3 collections in page 2, got %d", len(page2.Collections))
	}
}

// TestService_Route tests routing to collections
func TestService_Route(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// Create a collection with endpoint
	coll := &pb.Collection{
		Namespace:      "test",
		Name:           "routed",
		ServerEndpoint: "example.com:8080",
	}

	_, err := service.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Route to the collection
	req := &pb.RouteRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "routed",
		},
	}

	resp, err := service.Route(ctx, req)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d", resp.Status.Code)
	}

	if resp.ServerEndpoint != "example.com:8080" {
		t.Errorf("expected endpoint 'example.com:8080', got '%s'", resp.ServerEndpoint)
	}

	if resp.Collection == nil {
		t.Error("expected collection in response")
	}
}

func TestService_Route_DefaultEndpoint(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// Create collection without endpoint
	coll := &pb.Collection{
		Namespace: "test",
		Name:      "local",
	}

	_, err := service.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Route should return default endpoint
	req := &pb.RouteRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "local",
		},
	}

	resp, err := service.Route(ctx, req)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if resp.ServerEndpoint != "localhost:50051" {
		t.Errorf("expected default endpoint 'localhost:50051', got '%s'", resp.ServerEndpoint)
	}
}

func TestService_Route_NotFound(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	req := &pb.RouteRequest{
		Collection: &pb.NamespacedName{
			Namespace: "nonexistent",
			Name:      "missing",
		},
	}

	resp, err := service.Route(ctx, req)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if resp.Status.Code != 404 {
		t.Errorf("expected status 404, got %d", resp.Status.Code)
	}
}

func TestService_Route_NilCollection(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	req := &pb.RouteRequest{
		Collection: nil,
	}

	resp, err := service.Route(ctx, req)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if resp.Status.Code != 400 {
		t.Errorf("expected status 400, got %d", resp.Status.Code)
	}
}

// TestService_SearchCollections tests cross-collection search
func TestService_SearchCollections(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// Create test collections
	collections := []*pb.Collection{
		{Namespace: "test", Name: "coll1"},
		{Namespace: "test", Name: "coll2"},
		{Namespace: "other", Name: "coll3"},
	}

	for _, coll := range collections {
		_, err := service.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Search specific collections
	req := &pb.SearchCollectionsRequest{
		Namespace:       "test",
		CollectionNames: []string{"coll1", "coll2"},
		Limit:           10,
	}

	resp, err := service.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d", resp.Status.Code)
	}

	// Verify the response indicates searching the right collections
	if !strings.Contains(resp.Status.Message, "2 collections") {
		t.Errorf("expected message to indicate 2 collections, got: %s", resp.Status.Message)
	}
}

func TestService_SearchCollections_AllInNamespace(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// Create collections in different namespaces
	collections := []*pb.Collection{
		{Namespace: "prod", Name: "users"},
		{Namespace: "prod", Name: "orders"},
		{Namespace: "dev", Name: "users"},
	}

	for _, coll := range collections {
		_, err := service.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Search all collections in 'prod' namespace
	req := &pb.SearchCollectionsRequest{
		Namespace: "prod",
		Limit:     10,
	}

	resp, err := service.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if !strings.Contains(resp.Status.Message, "2 collections") {
		t.Errorf("expected message to indicate 2 collections, got: %s", resp.Status.Message)
	}
}

func TestService_SearchCollections_AllNamespaces(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// Create collections
	collections := []*pb.Collection{
		{Namespace: "ns1", Name: "coll1"},
		{Namespace: "ns2", Name: "coll2"},
		{Namespace: "ns3", Name: "coll3"},
	}

	for _, coll := range collections {
		_, err := service.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Search all collections (empty namespace)
	req := &pb.SearchCollectionsRequest{
		Namespace: "",
		Limit:     10,
	}

	resp, err := service.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if !strings.Contains(resp.Status.Message, "3 collections") {
		t.Errorf("expected message to indicate 3 collections, got: %s", resp.Status.Message)
	}
}

// TestService_Concurrency tests concurrent operations
func TestService_ConcurrentCreates(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	done := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			coll := &pb.Collection{
				Namespace: "concurrent",
				Name:      string(rune('a' + id)),
			}
			_, err := service.CreateCollection(ctx, coll)
			done <- err
		}(i)
	}

	// Wait for all creates
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent create failed: %v", err)
		}
	}

	// Verify all were created
	req := &pb.DiscoverRequest{Namespace: "concurrent"}
	resp, _ := service.Discover(ctx, req)

	if len(resp.Collections) != 10 {
		t.Errorf("expected 10 collections, got %d", len(resp.Collections))
	}
}
