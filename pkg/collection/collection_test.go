package collection

import (
    "os"
    "path/filepath"
    "testing"
    "time"

    pb "github.com/accretional/collector/gen/collector"
)

func TestNewCollection_Success(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "collection-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{
        BasePath: tempDir,
    }

    proto := &pb.Collection{
        Namespace: "test",
        Name:      "users",
        MessageType: &pb.MessageTypeRef{
            Namespace:   "test",
            MessageName: "User",
        },
        IndexedFields: []string{"email", "username"},
        Metadata: &pb.Metadata{
            Labels: map[string]string{
                "environment": "test",
            },
        },
    }

    coll, err := NewCollection(proto, opts)
    if err != nil {
        t.Fatalf("failed to create collection: %v", err)
    }
    defer coll.Close()

    // Verify collection properties
    if coll.GetNamespace() != "test" {
        t.Errorf("expected namespace 'test', got '%s'", coll.GetNamespace())
    }

    if coll.GetName() != "users" {
        t.Errorf("expected name 'users', got '%s'", coll.GetName())
    }

    // Verify paths exist
    expectedPath := filepath.Join(tempDir, "test", "users")
    if coll.GetPath() != expectedPath {
        t.Errorf("expected path '%s', got '%s'", expectedPath, coll.GetPath())
    }

    if _, err := os.Stat(coll.dbPath); os.IsNotExist(err) {
        t.Error("database file was not created")
    }

    if _, err := os.Stat(coll.filesPath); os.IsNotExist(err) {
        t.Error("files directory was not created")
    }

    // Verify metadata timestamps are set
    if coll.proto.Metadata.CreatedAt == nil {
        t.Error("created_at timestamp not set")
    }

    if coll.proto.Metadata.UpdatedAt == nil {
        t.Error("updated_at timestamp not set")
    }
}

func TestNewCollection_MissingNamespace(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "collection-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{
        BasePath: tempDir,
    }

    proto := &pb.Collection{
        Name: "users",
    }

    _, err = NewCollection(proto, opts)
    if err == nil {
        t.Error("expected error for missing namespace, got nil")
    }
}

func TestNewCollection_MissingName(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "collection-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{
        BasePath: tempDir,
    }

    proto := &pb.Collection{
        Namespace: "test",
    }

    _, err = NewCollection(proto, opts)
    if err == nil {
        t.Error("expected error for missing name, got nil")
    }
}

func TestLoadCollection_Success(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "collection-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{
        BasePath: tempDir,
    }

    // Create a collection
    originalProto := &pb.Collection{
        Namespace: "test",
        Name:      "users",
        MessageType: &pb.MessageTypeRef{
            Namespace:   "test",
            MessageName: "User",
        },
        IndexedFields: []string{"email", "username"},
        ServerEndpoint: "localhost:8080",
        Metadata: &pb.Metadata{
            Labels: map[string]string{
                "environment": "production",
                "version":     "1.0",
            },
        },
    }

    coll, err := NewCollection(originalProto, opts)
    if err != nil {
        t.Fatalf("failed to create collection: %v", err)
    }
    
    createdAt := coll.proto.Metadata.CreatedAt
    updatedAt := coll.proto.Metadata.UpdatedAt
    
    coll.Close()

    // Load the collection
    loaded, err := LoadCollection("test", "users", opts)
    if err != nil {
        t.Fatalf("failed to load collection: %v", err)
    }
    defer loaded.Close()

    // Verify all properties were loaded correctly
    if loaded.GetNamespace() != "test" {
        t.Errorf("expected namespace 'test', got '%s'", loaded.GetNamespace())
    }

    if loaded.GetName() != "users" {
        t.Errorf("expected name 'users', got '%s'", loaded.GetName())
    }

    if loaded.proto.MessageType == nil {
        t.Fatal("message type not loaded")
    }

    if loaded.proto.MessageType.Namespace != "test" {
        t.Errorf("expected message type namespace 'test', got '%s'", loaded.proto.MessageType.Namespace)
    }

    if loaded.proto.MessageType.MessageName != "User" {
        t.Errorf("expected message type name 'User', got '%s'", loaded.proto.MessageType.MessageName)
    }

    if len(loaded.proto.IndexedFields) != 2 {
        t.Errorf("expected 2 indexed fields, got %d", len(loaded.proto.IndexedFields))
    }

    if loaded.proto.ServerEndpoint != "localhost:8080" {
        t.Errorf("expected server endpoint 'localhost:8080', got '%s'", loaded.proto.ServerEndpoint)
    }

    if loaded.proto.Metadata.Labels["environment"] != "production" {
        t.Errorf("expected environment label 'production', got '%s'", loaded.proto.Metadata.Labels["environment"])
    }

    if loaded.proto.Metadata.CreatedAt.Seconds != createdAt.Seconds {
        t.Error("created_at timestamp not preserved")
    }

    if loaded.proto.Metadata.UpdatedAt.Seconds != updatedAt.Seconds {
        t.Error("updated_at timestamp not preserved")
    }
}

func TestLoadCollection_NotFound(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "collection-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{
        BasePath: tempDir,
    }

    _, err = LoadCollection("test", "nonexistent", opts)
    if err == nil {
        t.Error("expected error for non-existent collection, got nil")
    }
}

func TestToProto_WithoutRecords(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "collection-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{
        BasePath: tempDir,
    }

    originalProto := &pb.Collection{
        Namespace: "test",
        Name:      "users",
        MessageType: &pb.MessageTypeRef{
            Namespace:   "test",
            MessageName: "User",
        },
        IndexedFields: []string{"email"},
        ServerEndpoint: "localhost:8080",
        Metadata: &pb.Metadata{
            Labels: map[string]string{
                "version": "2.0",
            },
        },
    }

    coll, err := NewCollection(originalProto, opts)
    if err != nil {
        t.Fatalf("failed to create collection: %v", err)
    }
    defer coll.Close()

    proto, err := coll.ToProto(false)
    if err != nil {
        t.Fatalf("failed to convert to proto: %v", err)
    }

    if proto.Namespace != "test" {
        t.Errorf("expected namespace 'test', got '%s'", proto.Namespace)
    }

    if proto.Name != "users" {
        t.Errorf("expected name 'users', got '%s'", proto.Name)
    }

    if proto.MessageType.MessageName != "User" {
        t.Errorf("expected message name 'User', got '%s'", proto.MessageType.MessageName)
    }

    if proto.ServerEndpoint != "localhost:8080" {
        t.Errorf("expected server endpoint 'localhost:8080', got '%s'", proto.ServerEndpoint)
    }

    if proto.Metadata.Labels["version"] != "2.0" {
        t.Errorf("expected version label '2.0', got '%s'", proto.Metadata.Labels["version"])
    }
}

func TestCollection_Namespacing(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "collection-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{
        BasePath: tempDir,
    }

    // Create collections in different namespaces with same name
    namespaces := []string{"ns1", "ns2", "ns3"}
    collections := make([]*Collection, len(namespaces))

    for i, ns := range namespaces {
        proto := &pb.Collection{
            Namespace: ns,
            Name:      "shared_name",
            MessageType: &pb.MessageTypeRef{
                Namespace:   ns,
                MessageName: "Message",
            },
        }

        coll, err := NewCollection(proto, opts)
        if err != nil {
            t.Fatalf("failed to create collection for namespace %s: %v", ns, err)
        }
        collections[i] = coll
    }

    // Verify all collections exist independently
    for i, ns := range namespaces {
        expectedPath := filepath.Join(tempDir, ns, "shared_name")
        if collections[i].GetPath() != expectedPath {
            t.Errorf("namespace %s: expected path '%s', got '%s'", ns, expectedPath, collections[i].GetPath())
        }

        if _, err := os.Stat(collections[i].dbPath); os.IsNotExist(err) {
            t.Errorf("namespace %s: database file not created", ns)
        }
    }

    // Cleanup
    for _, coll := range collections {
        coll.Close()
    }
}

func TestCollection_MetadataTimestamps(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "collection-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{
        BasePath: tempDir,
    }

    // Create without explicit metadata
    proto := &pb.Collection{
        Namespace: "test",
        Name:      "timestamps_test",
    }

    coll, err := NewCollection(proto, opts)
    if err != nil {
        t.Fatalf("failed to create collection: %v", err)
    }
    defer coll.Close()

    if coll.proto.Metadata == nil {
        t.Fatal("metadata not initialized")
    }

    if coll.proto.Metadata.CreatedAt == nil {
        t.Error("created_at not set")
    }

    if coll.proto.Metadata.UpdatedAt == nil {
        t.Error("updated_at not set")
    }

    // Check timestamps are recent (within last second)
    now := time.Now().Unix()
    createdAt := coll.proto.Metadata.CreatedAt.Seconds
    updatedAt := coll.proto.Metadata.UpdatedAt.Seconds

    if now-createdAt > 1 {
        t.Errorf("created_at timestamp seems wrong: now=%d, created=%d", now, createdAt)
    }

    if now-updatedAt > 1 {
        t.Errorf("updated_at timestamp seems wrong: now=%d, updated=%d", now, updatedAt)
    }

    // CreatedAt and UpdatedAt should be the same initially
    if createdAt != updatedAt {
        t.Error("created_at and updated_at should be equal for new collection")
    }
}

func TestCollection_MultipleInstancesSameCollection(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "collection-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{
        BasePath: tempDir,
    }

    // Create a collection
    proto := &pb.Collection{
        Namespace: "test",
        Name:      "shared",
    }

    coll1, err := NewCollection(proto, opts)
    if err != nil {
        t.Fatalf("failed to create first instance: %v", err)
    }
    defer coll1.Close()

    // Load the same collection
    coll2, err := LoadCollection("test", "shared", opts)
    if err != nil {
        t.Fatalf("failed to load second instance: %v", err)
    }
    defer coll2.Close()

    // Both should point to the same paths
    if coll1.GetPath() != coll2.GetPath() {
        t.Error("collections point to different paths")
    }

    if coll1.dbPath != coll2.dbPath {
        t.Error("collections use different database paths")
    }
}

func TestTimestampConversion(t *testing.T) {
    now := time.Now()
    
    // Convert to proto
    protoTS := timestampProto(now)
    
    if protoTS == nil {
        t.Fatal("timestampProto returned nil")
    }

    // Convert back
    converted := timeFromProto(protoTS)
    
    // Should be very close (within a nanosecond due to precision)
    if now.Unix() != converted.Unix() {
        t.Errorf("seconds don't match: original=%d, converted=%d", now.Unix(), converted.Unix())
    }

    // Test nil handling
    nilTime := timeFromProto(nil)
    if !nilTime.IsZero() {
        t.Error("timeFromProto(nil) should return zero time")
    }
}

func TestCollection_Close(t *testing.T) {
    tempDir, err := os.MkdirTemp("", "collection-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tempDir)

    opts := CollectionOptions{
        BasePath: tempDir,
    }

    proto := &pb.Collection{
        Namespace: "test",
        Name:      "close_test",
    }

    coll, err := NewCollection(proto, opts)
    if err != nil {
        t.Fatalf("failed to create collection: %v", err)
    }

    // Close should succeed
    if err := coll.Close(); err != nil {
        t.Errorf("close failed: %v", err)
    }

    // Second close should also succeed (idempotent)
    if err := coll.Close(); err != nil {
        t.Errorf("second close failed: %v", err)
    }
}
