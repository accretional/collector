package collection

import (
    "encoding/json"
    "os"
    "testing"

    pb "github.com/accretional/collector/gen/collector"
)

func TestCreateRecord_Success(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    data := map[string]interface{}{
        "name":  "Alice",
        "email": "alice@example.com",
        "age":   30,
    }
    jsonData, _ := json.Marshal(data)

    record := &pb.CollectionRecord{
        Id:        "user-001",
        ProtoData: jsonData,
        DataUri:   "files/user-001.json",
        Metadata: &pb.Metadata{
            Labels: map[string]string{
                "role": "admin",
            },
        },
    }

    err := coll.CreateRecord(record)
    if err != nil {
        t.Fatalf("failed to create record: %v", err)
    }

    // Verify record can be retrieved
    retrieved, err := coll.GetRecord("user-001")
    if err != nil {
        t.Fatalf("failed to retrieve record: %v", err)
    }

    if retrieved.Id != "user-001" {
        t.Errorf("expected id 'user-001', got '%s'", retrieved.Id)
    }

    if string(retrieved.ProtoData) != string(jsonData) {
        t.Error("proto data doesn't match")
    }

    if retrieved.DataUri != "files/user-001.json" {
        t.Errorf("expected data uri 'files/user-001.json', got '%s'", retrieved.DataUri)
    }

    if retrieved.Metadata.Labels["role"] != "admin" {
        t.Errorf("expected role label 'admin', got '%s'", retrieved.Metadata.Labels["role"])
    }

    // Verify timestamps are set
    if retrieved.Metadata.CreatedAt == nil {
        t.Error("created_at not set")
    }

    if retrieved.Metadata.UpdatedAt == nil {
        t.Error("updated_at not set")
    }
}

func TestCreateRecord_MissingId(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    record := &pb.CollectionRecord{
        ProtoData: []byte(`{"name": "test"}`),
    }

    err := coll.CreateRecord(record)
    if err == nil {
        t.Error("expected error for missing id, got nil")
    }
}

func TestCreateRecord_DuplicateId(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    record := &pb.CollectionRecord{
        Id:        "dup-001",
        ProtoData: []byte(`{"name": "test"}`),
    }

    // First insert should succeed
    if err := coll.CreateRecord(record); err != nil {
        t.Fatalf("first insert failed: %v", err)
    }

    // Second insert with same ID should fail
    err := coll.CreateRecord(record)
    if err == nil {
        t.Error("expected error for duplicate id, got nil")
    }
}

func TestGetRecord_NotFound(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    _, err := coll.GetRecord("nonexistent")
    if err == nil {
        t.Error("expected error for non-existent record, got nil")
    }
}

func TestUpdateRecord_Success(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create initial record
    original := &pb.CollectionRecord{
        Id:        "update-001",
        ProtoData: []byte(`{"name": "Alice", "version": 1}`),
        Metadata: &pb.Metadata{
            Labels: map[string]string{"status": "draft"},
        },
    }

    if err := coll.CreateRecord(original); err != nil {
        t.Fatalf("failed to create record: %v", err)
    }

    createdAt := original.Metadata.CreatedAt

    // Update the record
    updated := &pb.CollectionRecord{
        Id:        "update-001",
        ProtoData: []byte(`{"name": "Alice Updated", "version": 2}`),
        DataUri:   "files/new-location.json",
        Metadata: &pb.Metadata{
            Labels: map[string]string{"status": "published"},
        },
    }

    if err := coll.UpdateRecord(updated); err != nil {
        t.Fatalf("failed to update record: %v", err)
    }

    // Retrieve and verify
    retrieved, err := coll.GetRecord("update-001")
    if err != nil {
        t.Fatalf("failed to retrieve updated record: %v", err)
    }

    var data map[string]interface{}
    json.Unmarshal(retrieved.ProtoData, &data)

    if data["name"] != "Alice Updated" {
        t.Errorf("expected name 'Alice Updated', got '%v'", data["name"])
    }

    if data["version"].(float64) != 2 {
        t.Errorf("expected version 2, got %v", data["version"])
    }

    if retrieved.DataUri != "files/new-location.json" {
        t.Errorf("expected data uri 'files/new-location.json', got '%s'", retrieved.DataUri)
    }

    if retrieved.Metadata.Labels["status"] != "published" {
        t.Errorf("expected status 'published', got '%s'", retrieved.Metadata.Labels["status"])
    }

    // CreatedAt should not change
    if retrieved.Metadata.CreatedAt.Seconds != createdAt.Seconds {
        t.Error("created_at timestamp should not change on update")
    }

    // UpdatedAt should be newer
    if retrieved.Metadata.UpdatedAt.Seconds <= createdAt.Seconds {
        t.Error("updated_at should be newer than created_at after update")
    }
}

func TestUpdateRecord_NotFound(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    record := &pb.CollectionRecord{
        Id:        "nonexistent",
        ProtoData: []byte(`{"name": "test"}`),
    }

    err := coll.UpdateRecord(record)
    if err == nil {
        t.Error("expected error for updating non-existent record, got nil")
    }
}

func TestUpdateRecord_MissingId(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    record := &pb.CollectionRecord{
        ProtoData: []byte(`{"name": "test"}`),
    }

    err := coll.UpdateRecord(record)
    if err == nil {
        t.Error("expected error for missing id, got nil")
    }
}

func TestDeleteRecord_Success(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create a record
    record := &pb.CollectionRecord{
        Id:        "delete-001",
        ProtoData: []byte(`{"name": "test"}`),
    }

    if err := coll.CreateRecord(record); err != nil {
        t.Fatalf("failed to create record: %v", err)
    }

    // Delete the record
    if err := coll.DeleteRecord("delete-001"); err != nil {
        t.Fatalf("failed to delete record: %v", err)
    }

    // Verify it's gone
    _, err := coll.GetRecord("delete-001")
    if err == nil {
        t.Error("expected error when getting deleted record, got nil")
    }
}

func TestDeleteRecord_NotFound(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    err := coll.DeleteRecord("nonexistent")
    if err == nil {
        t.Error("expected error for deleting non-existent record, got nil")
    }
}

func TestListRecords_Empty(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    records, err := coll.ListRecords(0, 10)
    if err != nil {
        t.Fatalf("failed to list records: %v", err)
    }

    if len(records) != 0 {
        t.Errorf("expected 0 records, got %d", len(records))
    }
}

func TestListRecords_Multiple(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create multiple records
    count := 5
    for i := 1; i <= count; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("record-%03d", i),
            ProtoData: []byte(fmt.Sprintf(`{"index": %d}`, i)),
        }
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record %d: %v", i, err)
        }
    }

    // List all records
    records, err := coll.ListRecords(0, 10)
    if err != nil {
        t.Fatalf("failed to list records: %v", err)
    }

    if len(records) != count {
        t.Errorf("expected %d records, got %d", count, len(records))
    }

    // Verify all IDs are present
    ids := make(map[string]bool)
    for _, record := range records {
        ids[record.Id] = true
    }

    for i := 1; i <= count; i++ {
        expectedId := fmt.Sprintf("record-%03d", i)
        if !ids[expectedId] {
            t.Errorf("record %s not found in list", expectedId)
        }
    }
}

func TestListRecords_Pagination(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create 10 records
    for i := 1; i <= 10; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("page-%03d", i),
            ProtoData: []byte(fmt.Sprintf(`{"index": %d}`, i)),
        }
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record %d: %v", i, err)
        }
    }

    // Test pagination
    tests := []struct {
        name           string
        offset         int
        limit          int
        expectedCount  int
    }{
        {"first page", 0, 3, 3},
        {"second page", 3, 3, 3},
        {"third page", 6, 3, 3},
        {"last page", 9, 3, 1},
        {"large limit", 0, 100, 10},
        {"zero limit (uses default)", 0, 0, 10},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            records, err := coll.ListRecords(tt.offset, tt.limit)
            if err != nil {
                t.Fatalf("failed to list records: %v", err)
            }

            if len(records) != tt.expectedCount {
                t.Errorf("expected %d records, got %d", tt.expectedCount, len(records))
            }
        })
    }
}

func TestListRecords_OrderedByCreatedAt(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create records with slight delay to ensure different timestamps
    ids := []string{"first", "second", "third"}
    for _, id := range ids {
        record := &pb.CollectionRecord{
            Id:        id,
            ProtoData: []byte(fmt.Sprintf(`{"name": "%s"}`, id)),
        }
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record %s: %v", id, err)
        }
    }

    // List records (should be ordered by created_at DESC)
    records, err := coll.ListRecords(0, 10)
    if err != nil {
        t.Fatalf("failed to list records: %v", err)
    }

    // Most recent should be first
    if records[0].Id != "third" {
        t.Errorf("expected first record to be 'third', got '%s'", records[0].Id)
    }

    // Check ordering
    for i := 1; i < len(records); i++ {
        if records[i].Metadata.CreatedAt.Seconds > records[i-1].Metadata.CreatedAt.Seconds {
            t.Error("records not properly ordered by created_at DESC")
        }
    }
}

func TestCountRecords(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Initially should be 0
    count, err := coll.CountRecords()
    if err != nil {
        t.Fatalf("failed to count records: %v", err)
    }

    if count != 0 {
        t.Errorf("expected 0 records, got %d", count)
    }

    // Create records
    for i := 1; i <= 7; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("count-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"index": %d}`, i)),
        }
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record %d: %v", i, err)
        }
    }

    // Count should be 7
    count, err = coll.CountRecords()
    if err != nil {
        t.Fatalf("failed to count records: %v", err)
    }

    if count != 7 {
        t.Errorf("expected 7 records, got %d", count)
    }

    // Delete one
    if err := coll.DeleteRecord("count-1"); err != nil {
        t.Fatalf("failed to delete record: %v", err)
    }

    // Count should be 6
    count, err = coll.CountRecords()
    if err != nil {
        t.Fatalf("failed to count records: %v", err)
    }

    if count != 6 {
        t.Errorf("expected 6 records after delete, got %d", count)
    }
}

func TestRecord_WithoutLabels(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    record := &pb.CollectionRecord{
        Id:        "no-labels",
        ProtoData: []byte(`{"name": "test"}`),
    }

    if err := coll.CreateRecord(record); err != nil {
        t.Fatalf("failed to create record: %v", err)
    }

    retrieved, err := coll.GetRecord("no-labels")
    if err != nil {
        t.Fatalf("failed to retrieve record: %v", err)
    }

    if retrieved.Metadata == nil {
        t.Fatal("metadata should be initialized")
    }

    // Labels can be nil or empty map
    if retrieved.Metadata.Labels != nil && len(retrieved.Metadata.Labels) > 0 {
        t.Error("expected no labels")
    }
}

func TestRecord_LargeProtoData(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create a large data structure
    largeData := make(map[string]interface{})
    for i := 0; i < 1000; i++ {
        largeData[fmt.Sprintf("field_%d", i)] = fmt.Sprintf("value_%d", i)
    }

    jsonData, err := json.Marshal(largeData)
    if err != nil {
        t.Fatalf("failed to marshal large data: %v", err)
    }

    record := &pb.CollectionRecord{
        Id:        "large",
        ProtoData: jsonData,
    }

    if err := coll.CreateRecord(record); err != nil {
        t.Fatalf("failed to create large record: %v", err)
    }

    retrieved, err := coll.GetRecord("large")
    if err != nil {
        t.Fatalf("failed to retrieve large record: %v", err)
    }

    if len(retrieved.ProtoData) != len(jsonData) {
        t.Errorf("proto data size mismatch: expected %d, got %d", len(jsonData), len(retrieved.ProtoData))
    }
}

func TestSaveAndLoadMetadata(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Save some metadata changes
    coll.proto.ServerEndpoint = "new-endpoint:9090"
    coll.proto.IndexedFields = append(coll.proto.IndexedFields, "new_field")
    coll.proto.Metadata.Labels["new_label"] = "new_value"

    if err := coll.saveMetadata(); err != nil {
        t.Fatalf("failed to save metadata: %v", err)
    }

    // Load it back
    loaded, err := coll.loadMetadata()
    if err != nil {
        t.Fatalf("failed to load metadata: %v", err)
    }

    if loaded.ServerEndpoint != "new-endpoint:9090" {
        t.Errorf("server endpoint not preserved: got '%s'", loaded.ServerEndpoint)
    }

    foundNewField := false
    for _, field := range loaded.IndexedFields {
        if field == "new_field" {
            foundNewField = true
            break
        }
    }
    if !foundNewField {
        t.Error("new indexed field not preserved")
    }

    if loaded.Metadata.Labels["new_label"] != "new_value" {
        t.Error("new label not preserved")
    }
}
