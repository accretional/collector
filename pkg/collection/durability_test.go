package collection

import (
    "bytes"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "testing"
    "time"

    pb "github.com/accretional/collector/gen/collector"
)

// Transaction & Atomicity Tests
func TestCreateRecord_AtomicityOnFailure(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Try to create a record with invalid data that would fail FTS indexing
    record := &pb.CollectionRecord{
        Id:        "atomic-test",
        ProtoData: []byte{0xFF, 0xFE}, // Invalid UTF-8
    }

    err := coll.CreateRecord(record)
    // Should handle the error gracefully
    if err != nil {
        // Verify record was not created
        _, getErr := coll.GetRecord("atomic-test")
        if getErr == nil {
            t.Error("record should not exist after failed create")
        }

        // Verify count is still 0
        count, _ := coll.CountRecords()
        if count != 0 {
            t.Errorf("expected 0 records after failed create, got %d", count)
        }
    }
}

func TestUpdateRecord_RollbackOnFailure(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create initial record
    original := &pb.CollectionRecord{
        Id:        "rollback-test",
        ProtoData: []byte(`{"version": 1}`),
    }

    if err := coll.CreateRecord(original); err != nil {
        t.Fatalf("failed to create record: %v", err)
    }

    // Verify original is there
    retrieved, _ := coll.GetRecord("rollback-test")
    if string(retrieved.ProtoData) != `{"version": 1}` {
        t.Error("original data not correct")
    }

    // Try to update with invalid data
    invalid := &pb.CollectionRecord{
        Id:        "rollback-test",
        ProtoData: []byte{0xFF, 0xFE}, // Invalid UTF-8
    }

    _ = coll.UpdateRecord(invalid) // May or may not fail depending on SQLite handling

    // Original data should still be retrievable
    retrieved, err := coll.GetRecord("rollback-test")
    if err != nil {
        t.Fatalf("failed to get record after failed update: %v", err)
    }

    // If the update failed, original should be preserved
    var data map[string]interface{}
    if err := json.Unmarshal(retrieved.ProtoData, &data); err != nil {
        t.Error("data should still be valid JSON")
    }
}

// Concurrent Access Tests

func TestConcurrentReads(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create test records
    for i := 1; i <= 10; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("concurrent-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
        }
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    // Concurrent reads
    var wg sync.WaitGroup
    errors := make(chan error, 100)
    numReaders := 10
    readsPerReader := 10

    for i := 0; i < numReaders; i++ {
        wg.Add(1)
        go func(readerID int) {
            defer wg.Done()
            for j := 0; j < readsPerReader; j++ {
                recordID := fmt.Sprintf("concurrent-%d", (j%10)+1)
                _, err := coll.GetRecord(recordID)
                if err != nil {
                    errors <- fmt.Errorf("reader %d: %w", readerID, err)
                }
            }
        }(i)
    }

    wg.Wait()
    close(errors)

    // Check for errors
    for err := range errors {
        t.Errorf("concurrent read error: %v", err)
    }
}

func TestConcurrentWrites_DifferentRecords(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    var wg sync.WaitGroup
    errors := make(chan error, 100)
    numWriters := 10

    // Each writer creates different records
    for i := 0; i < numWriters; i++ {
        wg.Add(1)
        go func(writerID int) {
            defer wg.Done()
            for j := 0; j < 5; j++ {
                record := &pb.CollectionRecord{
                    Id:        fmt.Sprintf("writer-%d-record-%d", writerID, j),
                    ProtoData: []byte(fmt.Sprintf(`{"writer": %d, "seq": %d}`, writerID, j)),
                }
                if err := coll.CreateRecord(record); err != nil {
                    errors <- fmt.Errorf("writer %d: %w", writerID, err)
                }
            }
        }(i)
    }

    wg.Wait()
    close(errors)

    // Check for errors
    errorCount := 0
    for err := range errors {
        t.Errorf("concurrent write error: %v", err)
        errorCount++
    }

    // Verify all records were created
    count, _ := coll.CountRecords()
    expectedCount := int64(numWriters * 5)
    if count != expectedCount && errorCount == 0 {
        t.Errorf("expected %d records, got %d", expectedCount, count)
    }
}

func TestConcurrentWrites_SameRecord(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create initial record
    initial := &pb.CollectionRecord{
        Id:        "contested",
        ProtoData: []byte(`{"version": 0}`),
    }
    if err := coll.CreateRecord(initial); err != nil {
        t.Fatalf("failed to create initial record: %v", err)
    }

    var wg sync.WaitGroup
    numWriters := 10

    // Multiple writers updating the same record
    for i := 0; i < numWriters; i++ {
        wg.Add(1)
        go func(writerID int) {
            defer wg.Done()
            record := &pb.CollectionRecord{
                Id:        "contested",
                ProtoData: []byte(fmt.Sprintf(`{"version": %d}`, writerID)),
            }
            _ = coll.UpdateRecord(record) // Some may fail, that's ok
        }(i)
    }

    wg.Wait()

    // Verify record still exists and is valid
    retrieved, err := coll.GetRecord("contested")
    if err != nil {
        t.Fatalf("record should still exist: %v", err)
    }

    var data map[string]interface{}
    if err := json.Unmarshal(retrieved.ProtoData, &data); err != nil {
        t.Error("data should still be valid JSON")
    }
}

func TestConcurrentReadWrite(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create initial records
    for i := 1; i <= 5; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("rw-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"counter": 0}`)),
        }
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    var wg sync.WaitGroup
    done := make(chan struct{})
    errors := make(chan error, 100)

    // Start readers
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for {
                select {
                case <-done:
                    return
                default:
                    _, err := coll.ListRecords(0, 10)
                    if err != nil {
                        errors <- err
                    }
                }
            }
        }()
    }

    // Start writers
    for i := 0; i < 3; i++ {
        wg.Add(1)
        go func(writerID int) {
            defer wg.Done()
            for j := 0; j < 10; j++ {
                recordID := fmt.Sprintf("rw-%d", (j%5)+1)
                record := &pb.CollectionRecord{
                    Id:        recordID,
                    ProtoData: []byte(fmt.Sprintf(`{"counter": %d}`, j)),
                }
                if err := coll.UpdateRecord(record); err != nil {
                    errors <- err
                }
                time.Sleep(time.Millisecond)
            }
        }(i)
    }

    // Let it run for a bit
    time.Sleep(100 * time.Millisecond)
    close(done)
    wg.Wait()
    close(errors)

    // Check for errors
    for err := range errors {
        t.Errorf("concurrent read/write error: %v", err)
    }
}

// Recovery & Resilience Tests

func TestRecovery_AfterAbnormalClose(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "recovery-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{BasePath: tempDir}
    proto := &pb.Collection{
        Namespace: "recovery",
        Name:      "test",
    }

    // Create collection and add data
    coll, err := NewCollection(proto, opts)
    if err != nil {
        t.Fatalf("failed to create collection: %v", err)
    }

    records := make([]*pb.CollectionRecord, 10)
    for i := 0; i < 10; i++ {
        records[i] = &pb.CollectionRecord{
            Id:        fmt.Sprintf("rec-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
        }
        if err := coll.CreateRecord(records[i]); err != nil {
            t.Fatalf("failed to create record %d: %v", i, err)
        }
    }

    // Simulate abnormal termination (don't call Close)
    // Just nil out the db connection
    coll.db = nil

    // Reopen the collection
    recovered, err := LoadCollection("recovery", "test", opts)
    if err != nil {
        t.Fatalf("failed to recover collection: %v", err)
    }
    defer recovered.Close()

    // Verify all data is still there
    count, err := recovered.CountRecords()
    if err != nil {
        t.Fatalf("failed to count records: %v", err)
    }

    if count != 10 {
        t.Errorf("expected 10 records after recovery, got %d", count)
    }

    // Verify each record
    for i := 0; i < 10; i++ {
        retrieved, err := recovered.GetRecord(fmt.Sprintf("rec-%d", i))
        if err != nil {
            t.Errorf("failed to retrieve record %d: %v", i, err)
        } else {
            var data map[string]interface{}
            json.Unmarshal(retrieved.ProtoData, &data)
            if data["id"] != float64(i) {
                t.Errorf("record %d has wrong data: %v", i, data)
            }
        }
    }
}

func TestRecovery_FTSIndexConsistency(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "fts-recovery-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{BasePath: tempDir}
    proto := &pb.Collection{
        Namespace: "fts",
        Name:      "test",
    }

    // Create and populate
    coll, err := NewCollection(proto, opts)
    if err != nil {
        t.Fatalf("failed to create collection: %v", err)
    }

    for i := 0; i < 5; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("search-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"content": "searchable term %d"}`, i)),
        }
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    // Close properly
    coll.Close()

    // Reopen
    reopened, err := LoadCollection("fts", "test", opts)
    if err != nil {
        t.Fatalf("failed to reopen collection: %v", err)
    }
    defer reopened.Close()

    // FTS search should still work
    results, err := reopened.Search(SearchQuery{
        FullText: "searchable",
        Limit:    10,
    })

    if err != nil {
        t.Fatalf("search failed after reopen: %v", err)
    }

    if len(results) != 5 {
        t.Errorf("expected 5 search results, got %d", len(results))
    }
}

// Data Corruption & Validation Tests

func TestInvalidJSON_Handling(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    invalidJSONs := [][]byte{
        []byte(`{"unclosed": `),
        []byte(`{invalid json}`),
        []byte(`[1,2,3,`),
        []byte(`"just a string"`),
        nil,
        []byte{},
    }

    for i, invalidJSON := range invalidJSONs {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("invalid-%d", i),
            ProtoData: invalidJSON,
        }

        // Should not panic, may or may not fail
        err := coll.CreateRecord(record)
        
        // If it succeeds, should be able to retrieve it
        if err == nil {
            retrieved, getErr := coll.GetRecord(record.Id)
            if getErr != nil {
                t.Errorf("record %d: created but can't retrieve: %v", i, getErr)
            }
            if !bytes.Equal(retrieved.ProtoData, invalidJSON) {
                t.Errorf("record %d: data corruption", i)
            }
        }
    }
}

func TestBinaryData_Handling(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Test with pure binary data
    binaryData := make([]byte, 1000)
    for i := range binaryData {
        binaryData[i] = byte(i % 256)
    }

    record := &pb.CollectionRecord{
        Id:        "binary",
        ProtoData: binaryData,
    }

    if err := coll.CreateRecord(record); err != nil {
        t.Fatalf("failed to create binary record: %v", err)
    }

    retrieved, err := coll.GetRecord("binary")
    if err != nil {
        t.Fatalf("failed to retrieve binary record: %v", err)
    }

    if !bytes.Equal(retrieved.ProtoData, binaryData) {
        t.Error("binary data was corrupted")
    }
}

func TestVeryLargeRecord(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create a 10MB record
    size := 10 * 1024 * 1024
    largeData := make([]byte, size)
    for i := range largeData {
        largeData[i] = byte('A' + (i % 26))
    }

    record := &pb.CollectionRecord{
        Id:        "large",
        ProtoData: largeData,
    }

    err := coll.CreateRecord(record)
    if err != nil {
        // It's ok if it fails due to size limits
        t.Logf("large record rejected (expected): %v", err)
        return
    }

    // If it succeeds, verify integrity
    retrieved, err := coll.GetRecord("large")
    if err != nil {
        t.Fatalf("failed to retrieve large record: %v", err)
    }

    if len(retrieved.ProtoData) != size {
        t.Errorf("size mismatch: expected %d, got %d", size, len(retrieved.ProtoData))
    }

    if !bytes.Equal(retrieved.ProtoData, largeData) {
        t.Error("large data was corrupted")
    }
}

// Path Safety Tests

func TestInvalidRecordIDs(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    invalidIDs := []string{
        "../escape",
        "../../etc/passwd",
        "/absolute/path",
        "with\x00null",
        strings.Repeat("a", 10000), // Very long ID
        "",                         // Empty (already tested but included)
        "with spaces",
        "with\ttabs",
        "with\nnewlines",
    }

    for _, id := range invalidIDs {
        record := &pb.CollectionRecord{
            Id:        id,
            ProtoData: []byte(`{"test": "data"}`),
        }

        err := coll.CreateRecord(record)
        
        // Empty ID should fail
        if id == "" && err == nil {
            t.Error("empty ID should be rejected")
        }

        // If accepted, verify it can be retrieved safely
        if err == nil {
            retrieved, getErr := coll.GetRecord(id)
            if getErr != nil {
                t.Errorf("ID '%s': created but can't retrieve: %v", id, getErr)
            }
            if retrieved.Id != id {
                t.Errorf("ID '%s': ID was changed to '%s'", id, retrieved.Id)
            }
        }
    }
}

func TestFilePathTraversal(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    dangerousPaths := []string{
        "../../../etc/passwd",
        "..\\..\\..\\windows\\system32",
        "/etc/passwd",
        "../../secrets.txt",
    }

    for _, path := range dangerousPaths {
        data := &pb.CollectionData{
            Name: filepath.Base(path),
            Content: &pb.CollectionData_Data{
                Data: []byte("malicious content"),
            },
        }

        err := coll.SaveFile(path, data)
        
        // Should either fail or sanitize the path
        if err == nil {
            // Verify file is within collection path
            fullPath := filepath.Join(coll.filesPath, path)
            cleanPath := filepath.Clean(fullPath)
            
            if !strings.HasPrefix(cleanPath, filepath.Clean(coll.filesPath)) {
                t.Errorf("path traversal vulnerability: %s escapes collection path", path)
            }
        }
    }
}

// Filesystem Safety Tests

func TestFileSystemConsistency_DeleteOrphanedFiles(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create a record with data_uri
    record := &pb.CollectionRecord{
        Id:        "with-file",
        ProtoData: []byte(`{"name": "test"}`),
        DataUri:   "files/data.bin",
    }

    if err := coll.CreateRecord(record); err != nil {
        t.Fatalf("failed to create record: %v", err)
    }

    // Create the actual file
    data := &pb.CollectionData{
        Name: "data.bin",
        Content: &pb.CollectionData_Data{
            Data: []byte("file content"),
        },
    }
    if err := coll.SaveFile("files/data.bin", data); err != nil {
        t.Fatalf("failed to save file: %v", err)
    }

    // Delete the record
    if err := coll.DeleteRecord("with-file"); err != nil {
        t.Fatalf("failed to delete record: %v", err)
    }

    // File still exists (we don't auto-delete)
    fullPath := filepath.Join(coll.filesPath, "files", "data.bin")
    if _, err := os.Stat(fullPath); os.IsNotExist(err) {
        t.Log("File was deleted (optional cleanup behavior)")
    } else {
        t.Log("File still exists (orphaned, but not corrupted)")
    }
}

func TestMetadataConsistency(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "metadata-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{BasePath: tempDir}
    proto := &pb.Collection{
        Namespace: "meta",
        Name:      "test",
        MessageType: &pb.MessageTypeRef{
            Namespace:   "meta",
            MessageName: "Test",
        },
        IndexedFields: []string{"field1", "field2"},
    }

    coll, err := NewCollection(proto, opts)
    if err != nil {
        t.Fatalf("failed to create collection: %v", err)
    }

    // Add some records
    for i := 0; i < 3; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("rec-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"data": %d}`, i)),
        }
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    coll.Close()

    // Reopen and verify metadata matches
    reopened, err := LoadCollection("meta", "test", opts)
    if err != nil {
        t.Fatalf("failed to reopen: %v", err)
    }
    defer reopened.Close()

    if reopened.proto.MessageType.MessageName != "Test" {
        t.Error("message type not preserved")
    }

    if len(reopened.proto.IndexedFields) != 2 {
        t.Errorf("indexed fields not preserved: got %d", len(reopened.proto.IndexedFields))
    }

    // Count should match
    count, _ := reopened.CountRecords()
    if count != 3 {
        t.Errorf("expected 3 records, got %d", count)
    }
}

// Stress Tests

func TestStress_ManySmallRecords(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping stress test in short mode")
    }

    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    numRecords := 1000
    start := time.Now()

    for i := 0; i < numRecords; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("stress-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"id": %d, "data": "record %d"}`, i, i)),
        }
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record %d: %v", i, err)
        }
    }

    duration := time.Since(start)
    t.Logf("Created %d records in %v (%.2f records/sec)",
        numRecords, duration, float64(numRecords)/duration.Seconds())

    // Verify count
    count, err := coll.CountRecords()
    if err != nil {
        t.Fatalf("failed to count records: %v", err)
    }

    if count != int64(numRecords) {
        t.Errorf("expected %d records, got %d", numRecords, count)
    }

    // Random access test
    for i := 0; i < 100; i++ {
        id := fmt.Sprintf("stress-%d", i*10)
        _, err := coll.GetRecord(id)
        if err != nil {
            t.Errorf("failed to retrieve record %s: %v", id, err)
        }
    }
}

func TestStress_RapidCreateDelete(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping stress test in short mode")
    }

    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    iterations := 100
    for i := 0; i < iterations; i++ {
        // Create
        record := &pb.CollectionRecord{
            Id:        "churn",
            ProtoData: []byte(fmt.Sprintf(`{"iteration": %d}`, i)),
        }
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create in iteration %d: %v", i, err)
        }

        // Verify
        retrieved, err := coll.GetRecord("churn")
        if err != nil {
            t.Fatalf("failed to retrieve in iteration %d: %v", i, err)
        }

        var data map[string]interface{}
        json.Unmarshal(retrieved.ProtoData, &data)
        if data["iteration"] != float64(i) {
            t.Errorf("iteration %d: wrong data", i)
        }

        // Delete
        if err := coll.DeleteRecord("churn"); err != nil {
            t.Fatalf("failed to delete in iteration %d: %v", i, err)
        }
    }

    // Should be empty at the end
    count, _ := coll.CountRecords()
    if count != 0 {
        t.Errorf("expected 0 records at end, got %d", count)
    }
}
