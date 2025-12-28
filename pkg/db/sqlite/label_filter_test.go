package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSearch_LabelFilters(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "label-filter-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(dbPath, collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create test records with different namespace labels
	testData := []struct {
		id        string
		namespace string
		data      string
	}{
		{"ns1-record1", "namespace1", "data1"},
		{"ns1-record2", "namespace1", "data2"},
		{"ns1-record3", "namespace1", "data3"},
		{"ns2-record1", "namespace2", "data1"},
		{"ns2-record2", "namespace2", "data2"},
		{"ns3-record1", "namespace3", "data1"},
	}

	for _, td := range testData {
		testProto := &collector.NamespacedName{Namespace: td.namespace, Name: td.data}
		data, _ := proto.Marshal(testProto)

		record := &collector.CollectionRecord{
			Id:        td.id,
			ProtoData: data,
			Metadata: &collector.Metadata{
				CreatedAt: &timestamppb.Timestamp{Seconds: 1000000},
				UpdatedAt: &timestamppb.Timestamp{Seconds: 1000000},
				Labels: map[string]string{
					"namespace": td.namespace,
					"type":      "test",
				},
			},
		}

		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record %s: %v", td.id, err)
		}
	}

	// Test 1: Filter by namespace1
	t.Run("FilterNamespace1", func(t *testing.T) {
		query := &collection.SearchQuery{
			LabelFilters: map[string]string{
				"namespace": "namespace1",
			},
		}

		results, err := store.Search(ctx, query)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) != 3 {
			t.Errorf("Expected 3 results for namespace1, got %d", len(results))
		}

		for _, result := range results {
			if result.Record.Id != "ns1-record1" && result.Record.Id != "ns1-record2" && result.Record.Id != "ns1-record3" {
				t.Errorf("Unexpected record ID: %s", result.Record.Id)
			}
		}
	})

	// Test 2: Filter by namespace2
	t.Run("FilterNamespace2", func(t *testing.T) {
		query := &collection.SearchQuery{
			LabelFilters: map[string]string{
				"namespace": "namespace2",
			},
		}

		results, err := store.Search(ctx, query)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Expected 2 results for namespace2, got %d", len(results))
		}
	})

	// Test 3: Filter by multiple labels
	t.Run("FilterMultipleLabels", func(t *testing.T) {
		query := &collection.SearchQuery{
			LabelFilters: map[string]string{
				"namespace": "namespace1",
				"type":      "test",
			},
		}

		results, err := store.Search(ctx, query)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) != 3 {
			t.Errorf("Expected 3 results with both labels, got %d", len(results))
		}
	})

	// Test 4: No matching labels
	t.Run("NoMatchingLabels", func(t *testing.T) {
		query := &collection.SearchQuery{
			LabelFilters: map[string]string{
				"namespace": "nonexistent",
			},
		}

		results, err := store.Search(ctx, query)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) != 0 {
			t.Errorf("Expected 0 results for nonexistent namespace, got %d", len(results))
		}
	})

	// Test 5: No filters (return all)
	t.Run("NoFilters", func(t *testing.T) {
		query := &collection.SearchQuery{}

		results, err := store.Search(ctx, query)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) != 6 {
			t.Errorf("Expected 6 results with no filters, got %d", len(results))
		}
	})
}
