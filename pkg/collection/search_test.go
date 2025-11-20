package collection_test // CHANGED from package collection

import (
	"context"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
)

func createTestRecord(t *testing.T, id string, jsonContent []byte) *pb.CollectionRecord {
	t.Helper()
	return &pb.CollectionRecord{
		Id:        id,
		ProtoData: jsonContent,
	}
}

func TestSearch_FullTextBasic(t *testing.T) {
	// Use the helper from setup_test.go
	coll, cleanup := setupTestCollection(t) 
	defer cleanup()
	ctx := context.Background()

	// Create test records
	records := []*pb.CollectionRecord{
		createTestRecord(t, "1", []byte(`{"name": "Alice", "bio": "distributed systems engineer"}`)),
		createTestRecord(t, "2", []byte(`{"name": "Bob", "bio": "cloud infrastructure manager"}`)),
	}

	for _, record := range records {
		if err := coll.CreateRecord(ctx, record); err != nil { // Added ctx
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Test Search
	results, err := coll.Search(ctx, &collection.SearchQuery{ // Added ctx, ptr to SearchQuery
		FullText: "distributed systems",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].Record.Id != "1" {
		t.Errorf("expected ID 1, got %s", results[0].Record.Id)
	}
}
