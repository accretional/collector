package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/protobuf/proto"
)

func TestBootstrapSystemCollections(t *testing.T) {
	ctx := context.Background()

	// Create temp directory for test
	tmpDir := t.TempDir()
	pathConfig := collection.NewPathConfig(tmpDir)

	// Bootstrap system collections
	sc, err := BootstrapSystemCollections(ctx, pathConfig)
	if err != nil {
		t.Fatalf("BootstrapSystemCollections failed: %v", err)
	}
	defer sc.Close()

	// Verify all collections are initialized
	if sc.CollectionRegistry == nil {
		t.Fatal("CollectionRegistry is nil")
	}
	if sc.TypeRegistry == nil {
		t.Fatal("TypeRegistry is nil")
	}
	if sc.Connections == nil {
		t.Fatal("Connections is nil")
	}
	if sc.Audit == nil {
		t.Fatal("Audit is nil")
	}
	if sc.Logs == nil {
		t.Fatal("Logs is nil")
	}

	// Verify collection metadata
	if sc.CollectionRegistry.GetNamespace() != "system" {
		t.Errorf("CollectionRegistry namespace = %s, want system", sc.CollectionRegistry.GetNamespace())
	}
	if sc.CollectionRegistry.GetName() != "collections" {
		t.Errorf("CollectionRegistry name = %s, want collections", sc.CollectionRegistry.GetName())
	}

	// Verify database files were created
	verifyDBExists(t, pathConfig, "system", "collections")
	verifyDBExists(t, pathConfig, "system", "types")
	verifyDBExists(t, pathConfig, "system", "connections")
	verifyDBExists(t, pathConfig, "system", "audit")
	verifyDBExists(t, pathConfig, "system", "logs")
}

func TestSelfReferentialCollectionRegistry(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	pathConfig := collection.NewPathConfig(tmpDir)

	sc, err := BootstrapSystemCollections(ctx, pathConfig)
	if err != nil {
		t.Fatalf("BootstrapSystemCollections failed: %v", err)
	}
	defer sc.Close()

	// Verify the collection registry references itself
	selfRecord, err := sc.CollectionRegistry.GetRecord(ctx, "system/collections")
	if err != nil {
		t.Fatalf("Failed to get self-reference record: %v", err)
	}

	// Unmarshal and verify
	var collMeta pb.Collection
	if err := proto.Unmarshal(selfRecord.ProtoData, &collMeta); err != nil {
		t.Fatalf("Failed to unmarshal collection: %v", err)
	}

	if collMeta.Namespace != "system" {
		t.Errorf("Self-reference namespace = %s, want system", collMeta.Namespace)
	}
	if collMeta.Name != "collections" {
		t.Errorf("Self-reference name = %s, want collections", collMeta.Name)
	}
	if collMeta.MessageType.MessageName != "Collection" {
		t.Errorf("Self-reference message type = %s, want Collection", collMeta.MessageType.MessageName)
	}

	// Verify metadata labels
	if selfRecord.Metadata.Labels["self-referential"] != "true" {
		t.Error("Self-reference should have self-referential=true label")
	}
}

func TestAllSystemCollectionsRegistered(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	pathConfig := collection.NewPathConfig(tmpDir)

	sc, err := BootstrapSystemCollections(ctx, pathConfig)
	if err != nil {
		t.Fatalf("BootstrapSystemCollections failed: %v", err)
	}
	defer sc.Close()

	// All 5 system collections should be registered in the collection registry
	expectedCollections := []struct {
		id          string
		namespace   string
		name        string
		messageType string
	}{
		{"system/collections", "system", "collections", "Collection"},
		{"system/types", "system", "types", "ValidationRule"},
		{"system/connections", "system", "connections", "Connection"},
		{"system/audit", "system", "audit", "AuditEvent"},
		{"system/logs", "system", "logs", "SystemLog"},
	}

	for _, expected := range expectedCollections {
		record, err := sc.CollectionRegistry.GetRecord(ctx, expected.id)
		if err != nil {
			t.Errorf("Failed to get collection %s: %v", expected.id, err)
			continue
		}

		var collMeta pb.Collection
		if err := proto.Unmarshal(record.ProtoData, &collMeta); err != nil {
			t.Errorf("Failed to unmarshal collection %s: %v", expected.id, err)
			continue
		}

		if collMeta.Namespace != expected.namespace {
			t.Errorf("Collection %s namespace = %s, want %s", expected.id, collMeta.Namespace, expected.namespace)
		}
		if collMeta.Name != expected.name {
			t.Errorf("Collection %s name = %s, want %s", expected.id, collMeta.Name, expected.name)
		}
		if collMeta.MessageType.MessageName != expected.messageType {
			t.Errorf("Collection %s message type = %s, want %s", expected.id, collMeta.MessageType.MessageName, expected.messageType)
		}

		// Verify system labels
		if record.Metadata.Labels["system"] != "true" {
			t.Errorf("Collection %s should have system=true label", expected.id)
		}
		if record.Metadata.Labels["bootstrap"] != "true" {
			t.Errorf("Collection %s should have bootstrap=true label", expected.id)
		}
	}
}

func TestSystemCollectionsUsable(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	pathConfig := collection.NewPathConfig(tmpDir)

	sc, err := BootstrapSystemCollections(ctx, pathConfig)
	if err != nil {
		t.Fatalf("BootstrapSystemCollections failed: %v", err)
	}
	defer sc.Close()

	// Test that we can write to audit collection
	auditEvent := &pb.AuditEvent{
		Id:        "test-audit-001",
		Namespace: "test",
		Operation: "CREATE",
		ActorId:   "test-user",
	}

	auditBytes, err := proto.Marshal(auditEvent)
	if err != nil {
		t.Fatalf("Failed to marshal audit event: %v", err)
	}

	auditRecord := &pb.CollectionRecord{
		Id:        "test-audit-001",
		ProtoData: auditBytes,
	}

	if err := sc.Audit.CreateRecord(ctx, auditRecord); err != nil {
		t.Fatalf("Failed to create audit record: %v", err)
	}

	// Verify we can read it back
	retrieved, err := sc.Audit.GetRecord(ctx, "test-audit-001")
	if err != nil {
		t.Fatalf("Failed to get audit record: %v", err)
	}

	if retrieved.Id != "test-audit-001" {
		t.Errorf("Retrieved record ID = %s, want test-audit-001", retrieved.Id)
	}

	// Test that we can write to connections collection
	connection := &pb.Connection{
		Id:                 "test-conn-001",
		SourceCollectorId:  "collector-1",
		TargetCollectorId:  "collector-2",
		Address:            "localhost:50051",
		SharedNamespaces:   []string{"test"},
	}

	connBytes, err := proto.Marshal(connection)
	if err != nil {
		t.Fatalf("Failed to marshal connection: %v", err)
	}

	connRecord := &pb.CollectionRecord{
		Id:        "test-conn-001",
		ProtoData: connBytes,
	}

	if err := sc.Connections.CreateRecord(ctx, connRecord); err != nil {
		t.Fatalf("Failed to create connection record: %v", err)
	}

	// Verify count
	count, err := sc.Connections.CountRecords(ctx)
	if err != nil {
		t.Fatalf("Failed to count connections: %v", err)
	}
	if count != 1 {
		t.Errorf("Connection count = %d, want 1", count)
	}
}

func TestBootstrapIdempotent(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	pathConfig := collection.NewPathConfig(tmpDir)

	// Bootstrap once
	sc1, err := BootstrapSystemCollections(ctx, pathConfig)
	if err != nil {
		t.Fatalf("First bootstrap failed: %v", err)
	}

	// Add a record
	testCollection := &pb.Collection{
		Namespace: "test",
		Name:      "test-coll",
	}
	testBytes, _ := proto.Marshal(testCollection)
	testRecord := &pb.CollectionRecord{
		Id:        "test/test-coll",
		ProtoData: testBytes,
	}
	if err := sc1.CollectionRegistry.CreateRecord(ctx, testRecord); err != nil {
		t.Fatalf("Failed to create test record: %v", err)
	}

	// Close first bootstrap
	sc1.Close()

	// Bootstrap again - should fail because databases already exist
	_, err = BootstrapSystemCollections(ctx, pathConfig)
	if err == nil {
		t.Error("Expected error when bootstrapping over existing databases, got nil")
	}
}

func TestBootstrapClose(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	pathConfig := collection.NewPathConfig(tmpDir)

	sc, err := BootstrapSystemCollections(ctx, pathConfig)
	if err != nil {
		t.Fatalf("BootstrapSystemCollections failed: %v", err)
	}

	// Close should not error
	if err := sc.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Collections should be closed - subsequent operations should fail
	_, err = sc.CollectionRegistry.GetRecord(ctx, "system/collections")
	if err == nil {
		t.Error("Expected error after close, got nil")
	}
}

func TestBootstrapDirectoryStructure(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	pathConfig := collection.NewPathConfig(tmpDir)

	sc, err := BootstrapSystemCollections(ctx, pathConfig)
	if err != nil {
		t.Fatalf("BootstrapSystemCollections failed: %v", err)
	}
	defer sc.Close()

	// Verify directory structure
	expectedDirs := []string{
		filepath.Join(tmpDir, "system"),
		filepath.Join(tmpDir, "files", "system", "collections"),
		filepath.Join(tmpDir, "files", "system", "types"),
		filepath.Join(tmpDir, "files", "system", "connections"),
		filepath.Join(tmpDir, "files", "system", "audit"),
		filepath.Join(tmpDir, "files", "system", "logs"),
	}

	for _, dir := range expectedDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("Expected directory %s does not exist", dir)
		}
	}
}

// Helper function to verify database file exists
func verifyDBExists(t *testing.T, pathConfig *collection.PathConfig, namespace, name string) {
	t.Helper()
	dbPath, err := pathConfig.CollectionDBPath(namespace, name)
	if err != nil {
		t.Fatalf("Failed to get DB path for %s/%s: %v", namespace, name, err)
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("Database file %s does not exist", dbPath)
	}
}

func BenchmarkBootstrap(b *testing.B) {
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		tmpDir := b.TempDir()
		pathConfig := collection.NewPathConfig(tmpDir)

		sc, err := BootstrapSystemCollections(ctx, pathConfig)
		if err != nil {
			b.Fatalf("BootstrapSystemCollections failed: %v", err)
		}
		sc.Close()
	}
}
