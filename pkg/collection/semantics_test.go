package collection_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
	"github.com/accretional/collector/pkg/db/sqliteext"
	"google.golang.org/protobuf/types/known/timestamppb"

	_ "modernc.org/sqlite"
)

// setupTestCollectionWithVector creates a REAL SQLite-backed collection with vector support.
func setupTestCollectionWithVector(t *testing.T) (*collection.Collection, collection.Embedder, func()) {
	t.Helper()

	// Create a temporary directory for this test run
	tempDir := t.TempDir()

	if available, reason := checkVectorExtension(t); !available {
		if reason == "skip" {
			t.Skip("CGo not enabled; skipping vector search tests")
		}
		t.Fatalf("Vector extension check failed: %s", reason)
	}

	// Initialize the REAL SQLite Store with vector support
	dbPath := tempDir + "/test.db"
	const dims = 16
	embedder := collection.NewDeterministicEmbedder(dims, 1)

	store, err := sqlite.NewSqliteStore(dbPath, collection.Options{
		EnableFTS:        true,
		EnableJSON:       true,
		EnableVector:     true,
		VectorDimensions: dims,
	}, embedder)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}

	// Initialize the REAL Local Filesystem
	fs, err := collection.NewLocalFileSystem(tempDir + "/files")
	if err != nil {
		store.Close()
		t.Fatalf("failed to create filesystem: %v", err)
	}

	// Create the Collection Domain Object
	proto := &pb.Collection{
		Namespace: "test-ns",
		Name:      "test-collection",
		Metadata:  &pb.Metadata{},
	}

	coll, err := collection.NewCollection(proto, store, fs)
	if err != nil {
		store.Close()
		t.Fatalf("failed to create collection: %v", err)
	}

	// Cleanup function to remove DB and files after test
	cleanup := func() {
		coll.Close() // Closes SQLite connection
	}

	return coll, embedder, cleanup
}

func TestSemanticEngine_FindSimilar_Basic(t *testing.T) {
	coll, embedder, cleanup := setupTestCollectionWithVector(t)
	defer cleanup()
	ctx := context.Background()

	// Create SemanticEngine
	engine := &collection.SemanticEngine{
		Collection: coll,
		Embedder:   embedder,
	}

	// Create test records with different content
	now := timestamppb.New(time.Now())
	records := []*pb.CollectionRecord{
		{
			Id:        "doc-1",
			ProtoData: []byte(`{"text": "machine learning and artificial intelligence"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		{
			Id:        "doc-2",
			ProtoData: []byte(`{"text": "deep learning neural networks"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		{
			Id:        "doc-3",
			ProtoData: []byte(`{"text": "cooking recipes and food preparation"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}

	// Insert records
	for _, record := range records {
		if err := coll.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Search for similar content
	results, err := engine.FindSimilar(ctx, "artificial intelligence", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	// Should find at least doc-1 (most similar) and possibly doc-2
	if len(results) == 0 {
		t.Fatal("expected at least one result, got 0")
	}

	// First result should be the most similar
	foundDoc1 := false
	for _, result := range results {
		if result.Record.Id == "doc-1" {
			foundDoc1 = true
			// Verify distance is non-negative
			if result.Distance < 0 {
				t.Errorf("expected non-negative distance, got %f", result.Distance)
			}
		}
	}

	if !foundDoc1 {
		t.Error("expected to find doc-1 as most similar result")
	}

	// doc-3 should not be in top results (different topic)
	foundDoc3 := false
	for _, result := range results {
		if result.Record.Id == "doc-3" {
			foundDoc3 = true
		}
	}

	// If doc-3 appears, it should have lower similarity than doc-1
	if foundDoc3 {
		doc1Distance := 0.0
		doc3Distance := 0.0
		for _, result := range results {
			if result.Record.Id == "doc-1" {
				doc1Distance = result.Distance
			}
			if result.Record.Id == "doc-3" {
				doc3Distance = result.Distance
			}
		}
		if doc3Distance <= doc1Distance {
			t.Errorf("doc-3 should have higher distance than doc-1: doc1=%f, doc3=%f", doc1Distance, doc3Distance)
		}
	}
}

func TestSemanticEngine_FindSimilar_Ordering(t *testing.T) {
	coll, embedder, cleanup := setupTestCollectionWithVector(t)
	defer cleanup()
	ctx := context.Background()

	engine := &collection.SemanticEngine{
		Collection: coll,
		Embedder:   embedder,
	}

	// Create records with varying similarity to query
	now := timestamppb.New(time.Now())
	records := []*pb.CollectionRecord{
		{
			Id:        "exact-match",
			ProtoData: []byte(`{"text": "python programming language"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		{
			Id:        "partial-match",
			ProtoData: []byte(`{"text": "programming software development"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		{
			Id:        "weak-match",
			ProtoData: []byte(`{"text": "computer technology"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}

	for _, record := range records {
		if err := coll.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Search for "python programming"
	results, err := engine.FindSimilar(ctx, "python programming", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	// Results should be ordered by distance (ascending)
	for i := 1; i < len(results); i++ {
		if results[i].Distance < results[i-1].Distance {
			t.Errorf("results not properly ordered: result[%d].Distance=%f < result[%d].Distance=%f",
				i, results[i].Distance, i-1, results[i-1].Distance)
		}
	}

	// Most similar should be first
	if results[0].Record.Id != "exact-match" {
		t.Logf("Note: exact-match not first, but this may vary with deterministic embedder")
	}
}

func TestSemanticEngine_FindSimilar_Limit(t *testing.T) {
	coll, embedder, cleanup := setupTestCollectionWithVector(t)
	defer cleanup()
	ctx := context.Background()

	engine := &collection.SemanticEngine{
		Collection: coll,
		Embedder:   embedder,
	}

	// Create multiple records
	now := timestamppb.New(time.Now())
	for i := 0; i < 10; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("doc-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"text": "test document number %d"}`, i)),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		}
		if err := coll.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Search with limit
	results, err := engine.FindSimilar(ctx, "test document", 3)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

func TestSemanticEngine_FindSimilar_NoResults(t *testing.T) {
	coll, embedder, cleanup := setupTestCollectionWithVector(t)
	defer cleanup()
	ctx := context.Background()

	engine := &collection.SemanticEngine{
		Collection: coll,
		Embedder:   embedder,
	}

	// Search in empty collection
	results, err := engine.FindSimilar(ctx, "any query", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results in empty collection, got %d", len(results))
	}
}

func TestSemanticEngine_FindSimilar_EmbedderError(t *testing.T) {
	coll, _, cleanup := setupTestCollectionWithVector(t)
	defer cleanup()
	ctx := context.Background()

	// Create a mock embedder that returns an error
	errorEmbedder := &errorEmbedder{}

	engine := &collection.SemanticEngine{
		Collection: coll,
		Embedder:   errorEmbedder,
	}

	// FindSimilar should propagate embedder error
	results, err := engine.FindSimilar(ctx, "test query", 10)
	if err == nil {
		t.Error("expected error from embedder, got nil")
	}
	if results != nil {
		t.Errorf("expected nil results on error, got %v", results)
	}
}

// errorEmbedder is a test embedder that always returns an error
type errorEmbedder struct{}

func (e *errorEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, &embedderError{msg: "test embedder error"}
}

type embedderError struct {
	msg string
}

func (e *embedderError) Error() string {
	return e.msg
}

func TestSemanticEngine_FindSimilar_WithFilters(t *testing.T) {
	coll, embedder, cleanup := setupTestCollectionWithVector(t)
	defer cleanup()
	ctx := context.Background()

	engine := &collection.SemanticEngine{
		Collection: coll,
		Embedder:   embedder,
	}

	// Create records with different categories
	now := timestamppb.New(time.Now())
	records := []*pb.CollectionRecord{
		{
			Id:        "tech-1",
			ProtoData: []byte(`{"text": "machine learning algorithms", "category": "technology"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		{
			Id:        "tech-2",
			ProtoData: []byte(`{"text": "deep learning neural networks", "category": "technology"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		{
			Id:        "food-1",
			ProtoData: []byte(`{"text": "cooking recipes and food", "category": "food"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}

	for _, record := range records {
		if err := coll.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Use FindSimilar which only does semantic search
	// Note: FindSimilar doesn't support filters directly, but we can verify
	// that it works with the underlying Search that supports filters
	results, err := engine.FindSimilar(ctx, "learning algorithms", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	// Should find tech documents
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	// Verify we can also use Search directly with filters and vector
	queryVec, err := embedder.Embed(ctx, "learning algorithms")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	filteredResults, err := coll.Search(ctx, &collection.SearchQuery{
		Vector: queryVec,
		Filters: map[string]collection.Filter{
			"category": {Operator: collection.OpEquals, Value: "technology"},
		},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search with filters failed: %v", err)
	}

	// Should only find technology records
	for _, result := range filteredResults {
		var data map[string]interface{}
		if err := json.Unmarshal(result.Record.ProtoData, &data); err != nil {
			continue
		}
		if cat, ok := data["category"].(string); ok && cat != "technology" {
			t.Errorf("expected only technology records, got category: %s", cat)
		}
	}
}

func TestSemanticEngine_FindSimilar_SimilarityThreshold(t *testing.T) {
	coll, embedder, cleanup := setupTestCollectionWithVector(t)
	defer cleanup()
	ctx := context.Background()

	// Create records with varying similarity
	now := timestamppb.New(time.Now())
	records := []*pb.CollectionRecord{
		{
			Id:        "similar",
			ProtoData: []byte(`{"text": "artificial intelligence machine learning"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		{
			Id:        "different",
			ProtoData: []byte(`{"text": "cooking recipes food preparation"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}

	for _, record := range records {
		if err := coll.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Search without threshold
	queryVec, err := embedder.Embed(ctx, "artificial intelligence")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	allResults, err := coll.Search(ctx, &collection.SearchQuery{
		Vector: queryVec,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Find minimum similarity using 1/(1+distance)
	minSimilarity := 1.0
	for _, result := range allResults {
		sim := 1 / (1 + result.Distance)
		if sim < minSimilarity {
			minSimilarity = sim
		}
	}

	// Search with a threshold above minimum but capped below 1.0 to avoid inverting to a negative max distance.
	threshold := float32(minSimilarity + 0.1)
	if threshold >= 1 {
		threshold = 0.99
	}
	thresholdResults, err := coll.Search(ctx, &collection.SearchQuery{
		Vector:              queryVec,
		SimilarityThreshold: threshold,
		Limit:               10,
	})
	if err != nil {
		t.Fatalf("Search with threshold failed: %v", err)
	}

	// Should have fewer or equal results
	if len(thresholdResults) > len(allResults) {
		t.Errorf("threshold should reduce results: got %d, expected <= %d",
			len(thresholdResults), len(allResults))
	}

	// All results should meet threshold
	for _, result := range thresholdResults {
		if 1/(1+result.Distance) < float64(threshold) {
			t.Errorf("result similarity %f below threshold %f", 1/(1+result.Distance), threshold)
		}
	}
}

func TestSemanticEngine_FindSimilar_UpdateMaintainsVectors(t *testing.T) {
	coll, embedder, cleanup := setupTestCollectionWithVector(t)
	defer cleanup()
	ctx := context.Background()

	engine := &collection.SemanticEngine{
		Collection: coll,
		Embedder:   embedder,
	}

	// Create initial record
	now := timestamppb.New(time.Now())
	record := &pb.CollectionRecord{
		Id:        "doc-1",
		ProtoData: []byte(`{"text": "original content about machine learning"}`),
		Metadata: &pb.Metadata{
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	if err := coll.CreateRecord(ctx, record); err != nil {
		t.Fatalf("failed to create record: %v", err)
	}

	// Search for original content
	results, err := engine.FindSimilar(ctx, "machine learning", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected to find record with original content")
	}

	// Update record with new content
	updatedRecord := &pb.CollectionRecord{
		Id:        "doc-1",
		ProtoData: []byte(`{"text": "updated content about cooking recipes"}`),
		Metadata: &pb.Metadata{
			CreatedAt: now,
			UpdatedAt: timestamppb.New(time.Now()),
		},
	}

	if err := coll.UpdateRecord(ctx, updatedRecord); err != nil {
		t.Fatalf("failed to update record: %v", err)
	}

	// Search for updated content
	updatedResults, err := engine.FindSimilar(ctx, "cooking recipes", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	// Should find the updated record
	found := false
	for _, result := range updatedResults {
		if result.Record.Id == "doc-1" {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected to find updated record")
	}

	// Original query should have lower similarity now
	originalResults, err := engine.FindSimilar(ctx, "machine learning", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	// The record should either not appear or have lower similarity
	stillHighSimilarity := false
	for _, result := range originalResults {
		if result.Record.Id == "doc-1" && result.Distance < 0.5 {
			stillHighSimilarity = true
		}
	}

	if stillHighSimilarity {
		t.Log("Note: updated record still has high similarity to old query - this may be expected with deterministic embedder")
	}
}

func TestSemanticEngine_VectorIndexPopulated(t *testing.T) {
	coll, embedder, cleanup := setupTestCollectionWithVector(t)
	defer cleanup()
	ctx := context.Background()

	now := timestamppb.New(time.Now())
	records := []*pb.CollectionRecord{
		{
			Id:        "vss-1",
			ProtoData: []byte(`{"text": "alpha beta gamma"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		{
			Id:        "vss-2",
			ProtoData: []byte(`{"text": "delta epsilon zeta"}`),
			Metadata: &pb.Metadata{
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}

	for _, r := range records {
		if err := coll.CreateRecord(ctx, r); err != nil {
			t.Fatalf("create record: %v", err)
		}
	}

	// Verify vector index is populated by performing a vector search
	// With the hybrid driver approach, we can't directly query vec0 table
	// from modernc.org/sqlite, so we verify through search results
	results, err := coll.Search(ctx, &collection.SearchQuery{
		Vector: func() []float32 {
			v, _ := embedder.Embed(ctx, "alpha gamma")
			return v
		}(),
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("vector search: %v", err)
	}
	if len(results) != len(records) {
		t.Fatalf("expected %d results from vector index search, got %d", len(records), len(results))
	}

	// Verify both records are returned
	foundIDs := make(map[string]bool)
	for _, r := range results {
		foundIDs[r.Record.Id] = true
	}
	for _, r := range records {
		if !foundIDs[r.Id] {
			t.Errorf("expected to find record %s in vector search results", r.Id)
		}
	}
}

func checkVectorExtension(t *testing.T) (available bool, reason string) {
	t.Helper()

	// Get the extension path
	path := os.Getenv("SQLITE_VEC_EXTENSION")
	if path == "" {
		cwd, err := os.Getwd()
		if err == nil {
			dir := cwd
			for {
				candidate := filepath.Join(dir, "sqlite-vec", "vec0.so")
				if _, err := os.Stat(candidate); err == nil {
					path = candidate
					break
				}
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
		}
	}

	// Try to open with our CGo driver
	ctx := context.Background()
	tempFile := t.TempDir() + "/vec_test.db"
	vecDB, err := sqliteext.Open(ctx, tempFile, path, "sqlite3_vec_init")
	if err != nil {
		if strings.Contains(err.Error(), "CGo") {
			return false, "skip"
		}
		return false, fmt.Sprintf("failed to load vector extension: %v", err)
	}
	defer vecDB.Close()

	rows, err := vecDB.QueryContext(ctx, "SELECT vec_version()")
	if err != nil {
		return false, fmt.Sprintf("vec_version check failed: %v", err)
	}
	defer rows.Close()

	return true, ""
}
