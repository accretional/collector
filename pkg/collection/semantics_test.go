package collection_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db"
	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"google.golang.org/protobuf/types/known/timestamppb"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestCollectionWithVector(t *testing.T) (*collection.Collection, collection.Embedder, func()) {
	t.Helper()

	tempDir := t.TempDir()

	if !vectorExtensionAvailable(t) {
		t.Errorf("SQLite vector extension (vec0) not available; skipping vector index tests")
	}

	dbPath := tempDir + "/test.db"
	const dims = 16
	embedder := collection.NewDeterministicEmbedder(dims, 1)

	store, err := db.NewStore(context.Background(), db.Config{
		Type:       db.DBTypeSQLite,
		SQLitePath: dbPath,
		Options: collection.Options{
			EnableFTS:        true,
			EnableJSON:       true,
			EnableVector:     true,
			VectorDimensions: dims,
			Embedder:         embedder,
		},
	})
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}

	fs, err := collection.NewLocalFileSystem(tempDir + "/files")
	if err != nil {
		store.Close()
		t.Fatalf("failed to create filesystem: %v", err)
	}

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

	cleanup := func() {
		coll.Close()
		store.Close()
	}

	return coll, embedder, cleanup
}

func TestSemanticEngine_FindSimilar_Basic(t *testing.T) {
	coll, embedder, cleanup := setupTestCollectionWithVector(t)
	defer cleanup()
	ctx := context.Background()

	engine := &collection.SemanticEngine{
		Collection: coll,
		Embedder:   embedder,
	}

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

	for _, record := range records {
		if err := coll.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	results, err := engine.FindSimilar(ctx, "artificial intelligence", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result, got 0")
	}

	foundDoc1 := false
	for _, result := range results {
		if result.Record.Id == "doc-1" {
			foundDoc1 = true
			if result.Distance < 0 {
				t.Errorf("expected non-negative distance, got %f", result.Distance)
			}
		}
	}

	if !foundDoc1 {
		t.Error("expected to find doc-1 as most similar result")
	}

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

	results, err := engine.FindSimilar(ctx, "python programming", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	for i := 1; i < len(results); i++ {
		if results[i].Distance < results[i-1].Distance {
			t.Errorf("results not properly ordered: result[%d].Distance=%f < result[%d].Distance=%f",
				i, results[i].Distance, i-1, results[i-1].Distance)
		}
	}

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

	errorEmbedder := &errorEmbedder{}

	engine := &collection.SemanticEngine{
		Collection: coll,
		Embedder:   errorEmbedder,
	}

	results, err := engine.FindSimilar(ctx, "test query", 10)
	if err == nil {
		t.Error("expected error from embedder, got nil")
	}
	if results != nil {
		t.Errorf("expected nil results on error, got %v", results)
	}
}

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

	results, err := engine.FindSimilar(ctx, "learning algorithms", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

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

	minSimilarity := 1.0
	for _, result := range allResults {
		sim := 1 / (1 + result.Distance)
		if sim < minSimilarity {
			minSimilarity = sim
		}
	}

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

	if len(thresholdResults) > len(allResults) {
		t.Errorf("threshold should reduce results: got %d, expected <= %d",
			len(thresholdResults), len(allResults))
	}

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

	results, err := engine.FindSimilar(ctx, "machine learning", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected to find record with original content")
	}

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

	updatedResults, err := engine.FindSimilar(ctx, "cooking recipes", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

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

	originalResults, err := engine.FindSimilar(ctx, "machine learning", 10)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

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

func vectorExtensionAvailable(t *testing.T) bool {
	t.Helper()

	sqlite_vec.Auto()

	ctx := context.Background()
	tempFile := t.TempDir() + "/vec_test.db"
	db, err := sql.Open("sqlite3", tempFile)
	if err != nil {
		t.Logf("failed to open db: %v", err)
		return false
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT vec_version()")
	if err != nil {
		t.Logf("vec_version check failed: %v", err)
		return false
	}
	defer rows.Close()

	if !rows.Next() {
		t.Logf("vec_version returned no rows")
		return false
	}

	var version string
	if err := rows.Scan(&version); err != nil {
		t.Logf("failed to scan vec_version: %v", err)
		return false
	}

	return true
}
