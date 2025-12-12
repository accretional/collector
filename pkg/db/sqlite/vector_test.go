package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestVectorSearch_WithDeterministicEmbedder(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")

	const dims = 16
	emb := collection.NewDeterministicEmbedder(dims, 1)
	opts := collection.Options{
		EnableJSON:       true,
		EnableVector:     true,
		VectorDimensions: dims,
	}

	store, err := NewSqliteStore(dbPath, opts, emb)
	if err != nil {
		t.Fatalf("NewSqliteStore: %v", err)
	}
	defer store.Close()

	now := timestamppb.New(time.Now())
	rec := &pb.CollectionRecord{
		Id:        "vec-1",
		ProtoData: []byte(`{"text": "hello world"}`),
		Metadata: &pb.Metadata{
			CreatedAt: now,
			UpdatedAt: now,
			Labels:    map[string]string{"k": "v"},
		},
		DataUri: "local://file",
	}

	if err := store.CreateRecord(context.Background(), rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}

	queryVec, err := emb.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	results, err := store.Search(context.Background(), &collection.SearchQuery{
		Vector: queryVec,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	got := results[0].Record
	if got.Id != rec.Id {
		t.Fatalf("expected id %s, got %s", rec.Id, got.Id)
	}
	if got.DataUri != rec.DataUri {
		t.Fatalf("expected data uri %s, got %s", rec.DataUri, got.DataUri)
	}
	if got.Metadata == nil || got.Metadata.CreatedAt == nil || got.Metadata.UpdatedAt == nil {
		t.Fatalf("metadata not hydrated: %+v", got.Metadata)
	}
	if got.Metadata.Labels["k"] != "v" {
		t.Fatalf("expected label v, got %s", got.Metadata.Labels["k"])
	}
	if results[0].Distance == 0 {
		t.Fatalf("expected non-zero similarity distance")
	}
}

func TestVectorSearch_DimensionMismatch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")

	emb := collection.NewDeterministicEmbedder(8, 1)
	opts := collection.Options{
		EnableJSON:       true,
		EnableVector:     true,
		VectorDimensions: 8,
	}

	store, err := NewSqliteStore(dbPath, opts, emb)
	if err != nil {
		t.Fatalf("NewSqliteStore: %v", err)
	}
	defer store.Close()

	now := timestamppb.Now()
	rec := &pb.CollectionRecord{
		Id:        "vec-2",
		ProtoData: []byte(`{"text": "hello"}`),
		Metadata: &pb.Metadata{
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	if err := store.CreateRecord(context.Background(), rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}

	// Deliberately use wrong dimension.
	badVec := make([]float32, 4)
	if _, err := store.Search(context.Background(), &collection.SearchQuery{Vector: badVec}); err == nil {
		t.Fatalf("expected dimension mismatch error, got nil")
	}
}
