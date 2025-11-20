package collection_test

import (
	"context"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
)

func TestRealSqliteIntegration(t *testing.T) {
	// This uses the real setup from setup_test.go
	coll, cleanup := setupTestCollection(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Create a record
	record := &pb.CollectionRecord{
		Id:        "rec-1",
		ProtoData: []byte(`{"key": "value"}`),
	}
	
	if err := coll.CreateRecord(ctx, record); err != nil {
		t.Fatalf("failed to create record: %v", err)
	}

	// 2. Retrieve it (verifies DB read)
	retrieved, err := coll.GetRecord(ctx, "rec-1")
	if err != nil {
		t.Fatalf("failed to get record: %v", err)
	}

	if retrieved.Id != "rec-1" {
		t.Errorf("expected id rec-1, got %s", retrieved.Id)
	}

	// 3. Verify internal storage path (just to prove it's not a mock)
	if coll.Store.Path() == "" {
		t.Error("store path should not be empty")
	}
}

func TestRealFTS(t *testing.T) {
	coll, cleanup := setupTestCollection(t)
	defer cleanup()
	ctx := context.Background()

	// Insert a record with searchable text (assuming s.CreateRecord populates jsontext)
	err := coll.CreateRecord(ctx, &pb.CollectionRecord{
		Id: "doc-1",
		// In a real app, this would be actual proto bytes that get marshaled to JSON
		// Our store implementation currently puts "{}" as a placeholder unless we passed it through
		ProtoData: []byte(`{}`), 
	})
	if err != nil { t.Fatal(err) }
	
	// Perform a search
	results, err := coll.Search(ctx, &collection.SearchQuery{
		FullText: "some text", 
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	
	// Just verify it runs without crashing, results depend on how we handle proto->json in the store
	if len(results) != 0 {
		t.Logf("Found %d results", len(results))
	}
}
