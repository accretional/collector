package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func setupTestStore(t *testing.T, opts collection.Options) (*Store, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	opts.EnableJSON = true // Always enable JSON for tests
	store, err := NewStore(dbPath, opts)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	return store, func() { store.Close() }
}

func createTestRecord(id string, data string, labels map[string]string) *pb.CollectionRecord {
	if labels == nil {
		labels = map[string]string{}
	}
	return &pb.CollectionRecord{
		Id: id,
		Metadata: &pb.Metadata{
			Labels:    labels,
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
		},
		ProtoData: []byte(data),
	}
}

func TestStore_CreateAndGetRecord(t *testing.T) {
	store, cleanup := setupTestStore(t, collection.Options{})
	defer cleanup()
	ctx := context.Background()

	record := createTestRecord("test-1", `{"name": "test"}`, nil)

	// Create
	if err := store.CreateRecord(ctx, record); err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}

	// Get
	got, err := store.GetRecord(ctx, "test-1")
	if err != nil {
		t.Fatalf("GetRecord failed: %v", err)
	}

	if got.Id != record.Id {
		t.Errorf("expected id %s, got %s", record.Id, got.Id)
	}
}

func TestStore_GetRecord_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t, collection.Options{})
	defer cleanup()
	ctx := context.Background()

	_, err := store.GetRecord(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent record, got nil")
	}
}

func TestStore_UpdateRecord(t *testing.T) {
	store, cleanup := setupTestStore(t, collection.Options{})
	defer cleanup()
	ctx := context.Background()

	// Create initial record
	record := createTestRecord("test-1", `{"name": "original"}`, nil)
	if err := store.CreateRecord(ctx, record); err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}

	// Update
	record.ProtoData = []byte(`{"name": "updated"}`)
	if err := store.UpdateRecord(ctx, record); err != nil {
		t.Fatalf("UpdateRecord failed: %v", err)
	}

	// Verify
	got, err := store.GetRecord(ctx, "test-1")
	if err != nil {
		t.Fatalf("GetRecord failed: %v", err)
	}

	if string(got.ProtoData) != `{"name": "updated"}` {
		t.Errorf("expected updated data, got %s", string(got.ProtoData))
	}
}

func TestStore_DeleteRecord(t *testing.T) {
	store, cleanup := setupTestStore(t, collection.Options{})
	defer cleanup()
	ctx := context.Background()

	// Create
	record := createTestRecord("test-1", `{"name": "test"}`, nil)
	if err := store.CreateRecord(ctx, record); err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}

	// Delete
	if err := store.DeleteRecord(ctx, "test-1"); err != nil {
		t.Fatalf("DeleteRecord failed: %v", err)
	}

	// Verify deleted
	_, err := store.GetRecord(ctx, "test-1")
	if err == nil {
		t.Error("expected error for deleted record, got nil")
	}
}

func TestStore_ListRecords(t *testing.T) {
	store, cleanup := setupTestStore(t, collection.Options{})
	defer cleanup()
	ctx := context.Background()

	// Create multiple records
	for i := 0; i < 5; i++ {
		record := createTestRecord(
			string(rune('a'+i)),
			`{"index": `+string(rune('0'+i))+`}`,
			nil,
		)
		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("CreateRecord failed: %v", err)
		}
	}

	// List all
	records, err := store.ListRecords(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListRecords failed: %v", err)
	}

	if len(records) != 5 {
		t.Errorf("expected 5 records, got %d", len(records))
	}
}

func TestStore_ListRecords_Pagination(t *testing.T) {
	store, cleanup := setupTestStore(t, collection.Options{})
	defer cleanup()
	ctx := context.Background()

	// Create 10 records
	for i := 0; i < 10; i++ {
		record := createTestRecord(
			string(rune('a'+i)),
			`{}`,
			nil,
		)
		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("CreateRecord failed: %v", err)
		}
	}

	// Get first page
	page1, err := store.ListRecords(ctx, 0, 3)
	if err != nil {
		t.Fatalf("ListRecords page 1 failed: %v", err)
	}
	if len(page1) != 3 {
		t.Errorf("expected 3 records in page 1, got %d", len(page1))
	}

	// Get second page
	page2, err := store.ListRecords(ctx, 3, 3)
	if err != nil {
		t.Fatalf("ListRecords page 2 failed: %v", err)
	}
	if len(page2) != 3 {
		t.Errorf("expected 3 records in page 2, got %d", len(page2))
	}

	// Verify no overlap
	if page1[0].Id == page2[0].Id {
		t.Error("pages should not overlap")
	}
}

func TestStore_CountRecords(t *testing.T) {
	store, cleanup := setupTestStore(t, collection.Options{})
	defer cleanup()
	ctx := context.Background()

	// Initially empty
	count, err := store.CountRecords(ctx)
	if err != nil {
		t.Fatalf("CountRecords failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 records, got %d", count)
	}

	// Add records
	for i := 0; i < 5; i++ {
		record := createTestRecord(string(rune('a'+i)), `{}`, nil)
		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("CreateRecord failed: %v", err)
		}
	}

	count, err = store.CountRecords(ctx)
	if err != nil {
		t.Fatalf("CountRecords failed: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 records, got %d", count)
	}
}

func TestStore_CreateRecord_DuplicateID(t *testing.T) {
	store, cleanup := setupTestStore(t, collection.Options{})
	defer cleanup()
	ctx := context.Background()

	record := createTestRecord("test-1", `{}`, nil)

	// First create should succeed
	if err := store.CreateRecord(ctx, record); err != nil {
		t.Fatalf("first CreateRecord failed: %v", err)
	}

	// Second create with same ID should fail
	err := store.CreateRecord(ctx, record)
	if err == nil {
		t.Error("expected error for duplicate ID, got nil")
	}
}

func TestStore_Labels(t *testing.T) {
	store, cleanup := setupTestStore(t, collection.Options{})
	defer cleanup()
	ctx := context.Background()

	labels := map[string]string{
		"env":     "prod",
		"version": "1.0",
	}
	record := createTestRecord("test-1", `{}`, labels)

	if err := store.CreateRecord(ctx, record); err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}

	got, err := store.GetRecord(ctx, "test-1")
	if err != nil {
		t.Fatalf("GetRecord failed: %v", err)
	}

	if got.Metadata.Labels["env"] != "prod" {
		t.Errorf("expected label env=prod, got %s", got.Metadata.Labels["env"])
	}
	if got.Metadata.Labels["version"] != "1.0" {
		t.Errorf("expected label version=1.0, got %s", got.Metadata.Labels["version"])
	}
}

func TestStore_Path(t *testing.T) {
	store, cleanup := setupTestStore(t, collection.Options{})
	defer cleanup()

	path := store.Path()
	if path == "" {
		t.Error("expected non-empty path")
	}
}

func TestStore_Checkpoint(t *testing.T) {
	store, cleanup := setupTestStore(t, collection.Options{})
	defer cleanup()
	ctx := context.Background()

	// Create some data
	record := createTestRecord("test-1", `{}`, nil)
	if err := store.CreateRecord(ctx, record); err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}

	// Checkpoint should not error
	if err := store.Checkpoint(ctx); err != nil {
		t.Errorf("Checkpoint failed: %v", err)
	}
}
