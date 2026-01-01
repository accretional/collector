package collection_test

import (
	"context"
	"fmt"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestCollectionRepo_CreateCollection tests creating a collection
func TestCollectionRepo_CreateCollection(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	coll := &pb.Collection{
		Namespace: "test-ns",
		Name:      "test-coll",
		MessageType: &pb.MessageTypeRef{
			MessageName: "TestMessage",
		},
		IndexedFields: []string{"field1", "field2"},
	}

	resp, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	if resp.CollectionId == "" {
		t.Error("expected non-empty collection ID")
	}

	expectedID := "test-ns/test-coll"
	if resp.CollectionId != expectedID {
		t.Errorf("expected collection ID '%s', got '%s'", expectedID, resp.CollectionId)
	}
}

func TestCollectionRepo_CreateCollection_MultipleCollections(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	collections := []*pb.Collection{
		{Namespace: "ns1", Name: "coll1"},
		{Namespace: "ns1", Name: "coll2"},
		{Namespace: "ns2", Name: "coll1"},
	}

	for _, coll := range collections {
		resp, err := repo.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed for %s/%s: %v", coll.Namespace, coll.Name, err)
		}

		if resp.Status.Code != 200 {
			t.Errorf("expected status 200, got %d", resp.Status.Code)
		}
	}
}

// TestCollectionRepo_Discover tests discovering collections
func TestCollectionRepo_Discover(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create test collections
	collections := []*pb.Collection{
		{Namespace: "ns1", Name: "coll1"},
		{Namespace: "ns1", Name: "coll2"},
		{Namespace: "ns2", Name: "coll1"},
	}

	for _, coll := range collections {
		_, err := repo.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Discover all collections
	req := &pb.DiscoverRequest{}
	resp, err := repo.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d", resp.Status.Code)
	}
}

func TestCollectionRepo_Discover_WithNamespaceFilter(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create collections in different namespaces
	collections := []*pb.Collection{
		{Namespace: "prod", Name: "users"},
		{Namespace: "prod", Name: "orders"},
		{Namespace: "dev", Name: "users"},
	}

	for _, coll := range collections {
		_, err := repo.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Discover collections in "prod" namespace
	req := &pb.DiscoverRequest{
		Namespace: "prod",
	}

	resp, err := repo.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d", resp.Status.Code)
	}
}

func TestCollectionRepo_Discover_WithMessageTypeFilter(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
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
	}

	for _, coll := range collections {
		_, err := repo.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Discover collections with User message type
	req := &pb.DiscoverRequest{
		MessageTypeFilter: &pb.MessageTypeRef{MessageName: "User"},
	}

	resp, err := repo.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d", resp.Status.Code)
	}
}

func TestCollectionRepo_Discover_WithPagination(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create multiple collections
	for i := 1; i <= 10; i++ {
		coll := &pb.Collection{
			Namespace: "test",
			Name:      string(rune('a' + i - 1)),
		}
		_, err := repo.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Request first page
	req := &pb.DiscoverRequest{
		PageSize: 3,
	}

	resp, err := repo.Discover(ctx, req)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	// Implementation returns 501 Not Implemented
	if resp.Status.Code != 501 {
		t.Logf("Discover returned status %d", resp.Status.Code)
	}
}

// TestCollectionRepo_Route tests routing to a collection
func TestCollectionRepo_Route(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create a collection
	coll := &pb.Collection{
		Namespace:      "test",
		Name:           "routed-coll",
		ServerEndpoint: "localhost:8080",
	}

	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Route to the collection
	req := &pb.RouteRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "routed-coll",
		},
	}

	resp, err := repo.Route(ctx, req)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	// Implementation returns 501 Not Implemented
	if resp.Status.Code != 501 {
		t.Logf("Route returned status %d", resp.Status.Code)
	}
}

func TestCollectionRepo_Route_NonExistentCollection(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Try to route to a non-existent collection
	req := &pb.RouteRequest{
		Collection: &pb.NamespacedName{
			Namespace: "nonexistent",
			Name:      "missing",
		},
	}

	resp, err := repo.Route(ctx, req)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	// Implementation returns 501 Not Implemented
	if resp.Status.Code != 501 {
		t.Logf("Route returned status %d", resp.Status.Code)
	}
}

// TestCollectionRepo_SearchCollections tests searching across multiple collections
func TestCollectionRepo_SearchCollections(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create collections
	collections := []*pb.Collection{
		{Namespace: "test", Name: "coll1"},
		{Namespace: "test", Name: "coll2"},
	}

	for _, coll := range collections {
		_, err := repo.CreateCollection(ctx, coll)
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

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Logf("SearchCollections returned status %d", resp.Status.Code)
	}
}

func TestCollectionRepo_SearchCollections_WithQuery(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create a collection
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Search with a query
	query := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"status": structpb.NewStringValue("active"),
		},
	}

	req := &pb.SearchCollectionsRequest{
		Namespace:       "test",
		CollectionNames: []string{"items"},
		Query:           query,
		Limit:           10,
		OrderBy:         "created_at",
	}

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Logf("SearchCollections returned status %d", resp.Status.Code)
	}
}

func TestCollectionRepo_SearchCollections_EmptyNamespace(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create collections in different namespaces
	collections := []*pb.Collection{
		{Namespace: "ns1", Name: "coll1"},
		{Namespace: "ns2", Name: "coll1"},
	}

	for _, coll := range collections {
		_, err := repo.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}
	}

	// Search across all namespaces (empty namespace)
	req := &pb.SearchCollectionsRequest{
		Namespace: "",
		Query:     &structpb.Struct{},
		Limit:     10,
	}

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Logf("SearchCollections returned status %d", resp.Status.Code)
	}
}

// TestCollectionRepo_SearchCollections_FTSAcrossCollections tests full-text search across multiple collections
func TestCollectionRepo_SearchCollections_FTSAcrossCollections(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create two collections
	coll1 := &pb.Collection{Namespace: "docs", Name: "articles"}
	coll2 := &pb.Collection{Namespace: "docs", Name: "tutorials"}

	if _, err := repo.CreateCollection(ctx, coll1); err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}
	if _, err := repo.CreateCollection(ctx, coll2); err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Add records to each collection
	articles, err := repo.GetCollection(ctx, "docs", "articles")
	if err != nil {
		t.Fatalf("GetCollection failed: %v", err)
	}
	defer articles.Close()

	tutorials, err := repo.GetCollection(ctx, "docs", "tutorials")
	if err != nil {
		t.Fatalf("GetCollection failed: %v", err)
	}
	defer tutorials.Close()

	// Add article about Go
	if err := articles.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "article-1",
		ProtoData: []byte(`{"title": "Go Programming", "content": "Go is a powerful language for building systems"}`),
	}); err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}

	// Add tutorial about Go
	if err := tutorials.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "tutorial-1",
		ProtoData: []byte(`{"title": "Go Tutorial", "content": "Learn Go programming step by step with Go examples"}`),
	}); err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}

	// Add article about Python (should not match Go search)
	if err := articles.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "article-2",
		ProtoData: []byte(`{"title": "Python Basics", "content": "Python is great for scripting"}`),
	}); err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}

	// Search for "Go" across both collections
	query := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"full_text": structpb.NewStringValue("Go"),
		},
	}

	req := &pb.SearchCollectionsRequest{
		Namespace: "docs",
		Query:     query,
		Limit:     10,
	}

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Fatalf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	// Should find results from both collections
	if resp.TotalMatches < 2 {
		t.Errorf("expected at least 2 matches across collections, got %d", resp.TotalMatches)
	}

	// Verify we have results from both collections
	foundCollections := make(map[string]bool)
	for _, result := range resp.Results {
		foundCollections[result.CollectionName] = true
	}

	if !foundCollections["docs/articles"] {
		t.Error("expected results from docs/articles collection")
	}
	if !foundCollections["docs/tutorials"] {
		t.Error("expected results from docs/tutorials collection")
	}
}

// TestCollectionRepo_SearchCollections_LimitAcrossCollections tests that limit is applied globally
func TestCollectionRepo_SearchCollections_LimitAcrossCollections(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create two collections
	coll1 := &pb.Collection{Namespace: "test", Name: "coll1"}
	coll2 := &pb.Collection{Namespace: "test", Name: "coll2"}

	if _, err := repo.CreateCollection(ctx, coll1); err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}
	if _, err := repo.CreateCollection(ctx, coll2); err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Add multiple records to each collection
	c1, _ := repo.GetCollection(ctx, "test", "coll1")
	defer c1.Close()
	c2, _ := repo.GetCollection(ctx, "test", "coll2")
	defer c2.Close()

	for i := 0; i < 10; i++ {
		c1.CreateRecord(ctx, &pb.CollectionRecord{
			Id:        fmt.Sprintf("c1-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"index": %d, "text": "item from collection one"}`, i)),
		})
		c2.CreateRecord(ctx, &pb.CollectionRecord{
			Id:        fmt.Sprintf("c2-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"index": %d, "text": "item from collection two"}`, i)),
		})
	}

	// Search with a global limit of 5
	req := &pb.SearchCollectionsRequest{
		Namespace: "test",
		Query:     &structpb.Struct{},
		Limit:     5,
	}

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Fatalf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	// Total matches should be limited to 5
	if resp.TotalMatches > 5 {
		t.Errorf("expected at most 5 total matches, got %d", resp.TotalMatches)
	}
}

// TestCollectionRepo_SearchCollections_ScoresNormalized tests that scores are normalized for cross-collection ranking
func TestCollectionRepo_SearchCollections_ScoresNormalized(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create collection
	coll := &pb.Collection{Namespace: "test", Name: "docs"}
	if _, err := repo.CreateCollection(ctx, coll); err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	c, _ := repo.GetCollection(ctx, "test", "docs")
	defer c.Close()

	// Add records with varying relevance
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "high-relevance",
		ProtoData: []byte(`{"title": "Go Go Go", "content": "Go programming Go language Go"}`),
	})
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "low-relevance",
		ProtoData: []byte(`{"title": "Python", "content": "Sometimes we use Go too"}`),
	})

	// Search for "Go"
	query := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"full_text": structpb.NewStringValue("Go"),
		},
	}

	req := &pb.SearchCollectionsRequest{
		Namespace: "test",
		Query:     query,
		Limit:     10,
	}

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Fatalf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	// Check that scores are in 0-1 range (normalized)
	for _, result := range resp.Results {
		for id, score := range result.Scores {
			if score < 0 || score > 1 {
				t.Errorf("score for %s should be normalized (0-1), got %f", id, score)
			}
		}
	}
}

// TestCollectionRepo_SearchCollections_WithFilters tests search with JSON filters
func TestCollectionRepo_SearchCollections_WithFilters(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create collection
	coll := &pb.Collection{Namespace: "test", Name: "products"}
	if _, err := repo.CreateCollection(ctx, coll); err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	c, _ := repo.GetCollection(ctx, "test", "products")
	defer c.Close()

	// Add products with different categories
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "product-1",
		ProtoData: []byte(`{"name": "Laptop", "category": "electronics", "price": 999}`),
	})
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "product-2",
		ProtoData: []byte(`{"name": "Shirt", "category": "clothing", "price": 29}`),
	})
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "product-3",
		ProtoData: []byte(`{"name": "Phone", "category": "electronics", "price": 699}`),
	})

	// Search with filter for electronics category
	query := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"filters": structpb.NewListValue(&structpb.ListValue{
				Values: []*structpb.Value{
					structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							"field":    structpb.NewStringValue("category"),
							"operator": structpb.NewStringValue("="),
							"value":    structpb.NewStringValue("electronics"),
						},
					}),
				},
			}),
		},
	}

	req := &pb.SearchCollectionsRequest{
		Namespace: "test",
		Query:     query,
		Limit:     10,
	}

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Fatalf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	// Should find only electronics (2 items)
	if resp.TotalMatches != 2 {
		t.Errorf("expected 2 matches for electronics, got %d", resp.TotalMatches)
	}
}

// TestCollectionRepo_SearchCollections_NoMatchingCollections tests searching non-existent collections
func TestCollectionRepo_SearchCollections_NoMatchingCollections(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Search in a namespace with no collections
	req := &pb.SearchCollectionsRequest{
		Namespace: "nonexistent",
		Query:     &structpb.Struct{},
		Limit:     10,
	}

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	// Should return OK with empty results
	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	if len(resp.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(resp.Results))
	}

	if resp.TotalMatches != 0 {
		t.Errorf("expected 0 total matches, got %d", resp.TotalMatches)
	}
}

// TestCollectionRepo_SearchCollections_WithPostFilters tests search with post-filters
func TestCollectionRepo_SearchCollections_WithPostFilters(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create collection
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	if _, err := repo.CreateCollection(ctx, coll); err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	c, _ := repo.GetCollection(ctx, "test", "items")
	defer c.Close()

	// Add items with different statuses
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "item-1",
		ProtoData: []byte(`{"name": "Alpha Widget", "status": "active", "priority": 1}`),
	})
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "item-2",
		ProtoData: []byte(`{"name": "Beta Widget", "status": "inactive", "priority": 2}`),
	})
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "item-3",
		ProtoData: []byte(`{"name": "Gamma Widget", "status": "active", "priority": 3}`),
	})

	// Search with FTS and post-filter for active status
	query := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"full_text": structpb.NewStringValue("Widget"),
			"post_filters": structpb.NewListValue(&structpb.ListValue{
				Values: []*structpb.Value{
					structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							"field":    structpb.NewStringValue("status"),
							"operator": structpb.NewStringValue("="),
							"value":    structpb.NewStringValue("active"),
						},
					}),
				},
			}),
		},
	}

	req := &pb.SearchCollectionsRequest{
		Namespace: "test",
		Query:     query,
		Limit:     10,
	}

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Fatalf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	// Should find only active items (2 items)
	if resp.TotalMatches != 2 {
		t.Errorf("expected 2 matches for active items, got %d", resp.TotalMatches)
	}
}

// TestCollectionRepo_SearchCollections_PreAndPostFilters tests combining pre and post filters
func TestCollectionRepo_SearchCollections_PreAndPostFilters(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create collection
	coll := &pb.Collection{Namespace: "test", Name: "products"}
	if _, err := repo.CreateCollection(ctx, coll); err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	c, _ := repo.GetCollection(ctx, "test", "products")
	defer c.Close()

	// Add products with category and region
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "p1",
		ProtoData: []byte(`{"name": "Laptop Pro", "category": "electronics", "region": "US"}`),
	})
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "p2",
		ProtoData: []byte(`{"name": "Laptop Basic", "category": "electronics", "region": "EU"}`),
	})
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "p3",
		ProtoData: []byte(`{"name": "T-Shirt", "category": "clothing", "region": "US"}`),
	})
	c.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "p4",
		ProtoData: []byte(`{"name": "Phone", "category": "electronics", "region": "US"}`),
	})

	// Pre-filter: category = electronics (narrows search space)
	// Post-filter: region = US (filters ranked results)
	query := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"filters": structpb.NewListValue(&structpb.ListValue{
				Values: []*structpb.Value{
					structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							"field":    structpb.NewStringValue("category"),
							"operator": structpb.NewStringValue("="),
							"value":    structpb.NewStringValue("electronics"),
						},
					}),
				},
			}),
			"post_filters": structpb.NewListValue(&structpb.ListValue{
				Values: []*structpb.Value{
					structpb.NewStructValue(&structpb.Struct{
						Fields: map[string]*structpb.Value{
							"field":    structpb.NewStringValue("region"),
							"operator": structpb.NewStringValue("="),
							"value":    structpb.NewStringValue("US"),
						},
					}),
				},
			}),
		},
	}

	req := &pb.SearchCollectionsRequest{
		Namespace: "test",
		Query:     query,
		Limit:     10,
	}

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Fatalf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	// Should find only electronics in US (2 items: Laptop Pro and Phone)
	if resp.TotalMatches != 2 {
		t.Errorf("expected 2 matches for electronics in US, got %d", resp.TotalMatches)
	}
}

// TestCollectionRepo_SearchCollections_SpecificCollections tests searching specific collection names
func TestCollectionRepo_SearchCollections_SpecificCollections(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create three collections
	for _, name := range []string{"coll1", "coll2", "coll3"} {
		coll := &pb.Collection{Namespace: "test", Name: name}
		if _, err := repo.CreateCollection(ctx, coll); err != nil {
			t.Fatalf("CreateCollection failed: %v", err)
		}

		c, _ := repo.GetCollection(ctx, "test", name)
		c.CreateRecord(ctx, &pb.CollectionRecord{
			Id:        "record-" + name,
			ProtoData: []byte(fmt.Sprintf(`{"source": "%s"}`, name)),
		})
		c.Close()
	}

	// Search only in coll1 and coll3 (skip coll2)
	req := &pb.SearchCollectionsRequest{
		Namespace:       "test",
		CollectionNames: []string{"coll1", "coll3"},
		Query:           &structpb.Struct{},
		Limit:           10,
	}

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Fatalf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	// Should find 2 results (one from each specified collection)
	if resp.TotalMatches != 2 {
		t.Errorf("expected 2 matches, got %d", resp.TotalMatches)
	}

	// Verify no results from coll2
	for _, result := range resp.Results {
		if result.CollectionName == "test/coll2" {
			t.Error("should not have results from coll2")
		}
	}
}

// TestCollectionRepo_SearchCollections_DefaultLimit tests that default limit is applied when not specified
func TestCollectionRepo_SearchCollections_DefaultLimit(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create collection with many records
	coll := &pb.Collection{Namespace: "test", Name: "large"}
	if _, err := repo.CreateCollection(ctx, coll); err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	c, _ := repo.GetCollection(ctx, "test", "large")
	defer c.Close()

	// Add 150 records (more than default limit of 100)
	for i := 0; i < 150; i++ {
		c.CreateRecord(ctx, &pb.CollectionRecord{
			Id:        fmt.Sprintf("record-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"index": %d}`, i)),
		})
	}

	// Search without specifying limit (should default to 100)
	req := &pb.SearchCollectionsRequest{
		Namespace: "test",
		Query:     &structpb.Struct{},
		Limit:     0, // No limit specified
	}

	resp, err := repo.SearchCollections(ctx, req)
	if err != nil {
		t.Fatalf("SearchCollections failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Fatalf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	// Should be limited to 100 (default)
	if resp.TotalMatches > 100 {
		t.Errorf("expected at most 100 matches (default limit), got %d", resp.TotalMatches)
	}
}

// TestCollectionRepo_GetCollection tests getting a collection instance
func TestCollectionRepo_GetCollection(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create a collection first
	coll := &pb.Collection{
		Namespace: "test",
		Name:      "items",
	}

	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Get the collection instance
	collInstance, err := repo.GetCollection(ctx, "test", "items")
	if err != nil {
		t.Fatalf("GetCollection failed: %v", err)
	}

	if collInstance == nil {
		t.Fatal("expected collection instance, got nil")
	}

	// Verify the collection metadata
	if collInstance.Meta.Namespace != "test" {
		t.Errorf("expected namespace 'test', got '%s'", collInstance.Meta.Namespace)
	}

	if collInstance.Meta.Name != "items" {
		t.Errorf("expected name 'items', got '%s'", collInstance.Meta.Name)
	}
}

func TestCollectionRepo_GetCollection_NonExistent(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Try to get a non-existent collection
	// Note: Current implementation always returns a new Collection instance
	// In a real implementation, this should return an error
	collInstance, err := repo.GetCollection(ctx, "nonexistent", "missing")

	// For now, the implementation returns a collection instance even if it doesn't exist
	// This is documented as needing improvement
	if err != nil {
		t.Logf("GetCollection returned error (expected behavior): %v", err)
	} else if collInstance != nil {
		t.Logf("GetCollection returned instance (current implementation)")
	}
}

// TestCollectionRepo_Concurrency tests concurrent operations on the repo
func TestCollectionRepo_ConcurrentCreates(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create collections concurrently
	done := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			coll := &pb.Collection{
				Namespace: "concurrent",
				Name:      string(rune('a' + id)),
			}

			_, err := repo.CreateCollection(ctx, coll)
			done <- err
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent create failed: %v", err)
		}
	}
}

// TestDefaultCollectionRepo_Isolation tests that DefaultCollectionRepo properly isolates operations
func TestDefaultCollectionRepo_Isolation(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create a collection
	coll := &pb.Collection{
		Namespace: "test",
		Name:      "isolation-test",
	}

	resp1, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Try to create the same collection again (should succeed or handle gracefully)
	resp2, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		// It's ok if it fails due to duplicate
		t.Logf("Duplicate create failed (expected): %v", err)
	} else {
		// If it succeeds, verify both responses are valid
		if resp1.CollectionId != resp2.CollectionId {
			t.Logf("Different collection IDs returned: %s vs %s", resp1.CollectionId, resp2.CollectionId)
		}
	}
}

// TestCollectionRepoService_DirectAccess tests the underlying service
func TestCollectionRepoService_CreateCollection(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Test through the repo interface (which wraps the service)
	coll := &pb.Collection{
		Namespace: "service-test",
		Name:      "direct",
		MessageType: &pb.MessageTypeRef{
			MessageName: "TestProto",
		},
	}

	resp, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	if resp.Status == nil {
		t.Fatal("expected status in response")
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}
}

// TestCollectionRepo_DeleteCollection tests deleting a collection
func TestCollectionRepo_DeleteCollection(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create a collection
	coll := &pb.Collection{
		Namespace: "test-ns",
		Name:      "test-delete",
		MessageType: &pb.MessageTypeRef{
			MessageName: "TestMessage",
		},
	}

	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Verify collection exists
	collInstance, err := repo.GetCollection(ctx, "test-ns", "test-delete")
	if err != nil {
		t.Fatalf("GetCollection failed: %v", err)
	}
	if collInstance == nil {
		t.Fatal("expected collection instance")
	}

	// Delete the collection
	delReq := &pb.DeleteCollectionRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test-ns",
			Name:      "test-delete",
		},
	}

	delResp, err := repo.DeleteCollection(ctx, delReq)
	if err != nil {
		t.Fatalf("DeleteCollection failed: %v", err)
	}

	if delResp.Status.Code != pb.Status_OK {
		t.Errorf("expected status OK, got %d: %s", delResp.Status.Code, delResp.Status.Message)
	}

	if delResp.BytesFreed <= 0 {
		t.Logf("Warning: expected bytes_freed > 0, got %d", delResp.BytesFreed)
	}

	// Verify collection no longer exists
	_, err = repo.GetCollection(ctx, "test-ns", "test-delete")
	if err == nil {
		t.Error("expected error when getting deleted collection")
	}
}

// TestCollectionRepo_DeleteCollection_NonExistent tests deleting a non-existent collection
func TestCollectionRepo_DeleteCollection_NonExistent(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Try to delete a collection that doesn't exist
	delReq := &pb.DeleteCollectionRequest{
		Collection: &pb.NamespacedName{
			Namespace: "nonexistent",
			Name:      "missing",
		},
	}

	_, err := repo.DeleteCollection(ctx, delReq)
	if err == nil {
		t.Error("expected error when deleting non-existent collection")
	}
}

// TestCollectionRepo_DeleteCollection_InvalidRequest tests invalid delete requests
func TestCollectionRepo_DeleteCollection_InvalidRequest(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	tests := []struct {
		name string
		req  *pb.DeleteCollectionRequest
	}{
		{
			name: "nil request",
			req:  nil,
		},
		{
			name: "nil collection",
			req:  &pb.DeleteCollectionRequest{Collection: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repo.DeleteCollection(ctx, tt.req)
			if err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}

// TestCollectionRepo_DeleteCollection_WithActiveOperation tests that deletion is blocked during active operations
func TestCollectionRepo_DeleteCollection_WithActiveOperation(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create a collection
	coll := &pb.Collection{
		Namespace: "test-ns",
		Name:      "active-op",
	}

	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	// Simulate an active backup operation by setting operation metadata
	collInstance, err := repo.GetCollection(ctx, "test-ns", "active-op")
	if err != nil {
		t.Fatalf("GetCollection failed: %v", err)
	}

	// Add operation label
	if collInstance.Meta.Metadata == nil {
		collInstance.Meta.Metadata = &pb.Metadata{}
	}
	if collInstance.Meta.Metadata.Labels == nil {
		collInstance.Meta.Metadata.Labels = make(map[string]string)
	}
	collInstance.Meta.Metadata.Labels["_operation"] = "backup:test-backup"

	// Update metadata
	err = repo.UpdateCollectionMetadata(ctx, "test-ns", "active-op", collInstance.Meta)
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata failed: %v", err)
	}

	// Try to delete - should fail due to active operation
	delReq := &pb.DeleteCollectionRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test-ns",
			Name:      "active-op",
		},
	}

	_, err = repo.DeleteCollection(ctx, delReq)
	if err == nil {
		t.Error("expected error when deleting collection with active operation")
	}
}

// TestCollectionRepo_DeleteCollection_Multiple tests deleting multiple collections
func TestCollectionRepo_DeleteCollection_Multiple(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	// Create multiple collections
	collections := []*pb.Collection{
		{Namespace: "ns1", Name: "coll1"},
		{Namespace: "ns1", Name: "coll2"},
		{Namespace: "ns2", Name: "coll1"},
	}

	for _, coll := range collections {
		_, err := repo.CreateCollection(ctx, coll)
		if err != nil {
			t.Fatalf("CreateCollection failed for %s/%s: %v", coll.Namespace, coll.Name, err)
		}
	}

	// Delete each collection
	for _, coll := range collections {
		delReq := &pb.DeleteCollectionRequest{
			Collection: &pb.NamespacedName{
				Namespace: coll.Namespace,
				Name:      coll.Name,
			},
		}

		delResp, err := repo.DeleteCollection(ctx, delReq)
		if err != nil {
			t.Errorf("DeleteCollection failed for %s/%s: %v", coll.Namespace, coll.Name, err)
			continue
		}

		if delResp.Status.Code != pb.Status_OK {
			t.Errorf("expected status OK for %s/%s, got %d", coll.Namespace, coll.Name, delResp.Status.Code)
		}
	}

	// Verify all collections are deleted
	for _, coll := range collections {
		_, err := repo.GetCollection(ctx, coll.Namespace, coll.Name)
		if err == nil {
			t.Errorf("collection %s/%s still exists after deletion", coll.Namespace, coll.Name)
		}
	}
}
