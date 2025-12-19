package collection_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
)

func TestMigration_OldToNewStructure(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "migration-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create old structure: {tempDir}/collections/ns1/coll1/data.db
	oldDBPath := filepath.Join(tempDir, "collections", "ns1", "coll1", "data.db")
	if err := os.MkdirAll(filepath.Dir(oldDBPath), 0755); err != nil {
		t.Fatal(err)
	}

	// Create a real database at old path
	oldStore, err := sqlite.NewSqliteStore(oldDBPath, collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatal(err)
	}
	oldStore.Close()

	// Create path config and registry
	pathConfig := collection.NewPathConfig(tempDir)
	registryPath := pathConfig.RegistryDBPath()
	if err := os.MkdirAll(filepath.Dir(registryPath), 0755); err != nil {
		t.Fatal(err)
	}

	registryStore, err := collection.NewSqliteRegistryStore(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer registryStore.Close()

	// Run migration
	migrator := collection.NewMigrator(pathConfig, registryStore)
	report, err := migrator.MigrateAll(context.Background())
	if err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Verify results
	if report.Migrated != 1 {
		t.Errorf("Expected 1 migrated, got %d", report.Migrated)
	}
	if report.Failed != 0 {
		t.Errorf("Expected 0 failed, got %d", report.Failed)
	}

	// Check new structure exists
	newDBPath := pathConfig.CollectionDBPath("ns1", "coll1")
	if _, err := os.Stat(newDBPath); os.IsNotExist(err) {
		t.Errorf("New database not found at %s", newDBPath)
	}

	// Check old structure renamed to .old
	oldDirBackup := filepath.Join(tempDir, "collections", "ns1", "coll1.old")
	if _, err := os.Stat(oldDirBackup); os.IsNotExist(err) {
		t.Errorf("Old directory not renamed to .old at %s", oldDirBackup)
	}

	// Verify registry entry
	meta, err := registryStore.GetCollection(context.Background(), "ns1", "coll1")
	if err != nil {
		t.Fatalf("Collection not in registry: %v", err)
	}
	if meta.DBPath != "ns1/coll1.db" {
		t.Errorf("Unexpected DB path in registry: %s", meta.DBPath)
	}
}

func TestMigration_MultipleCollections(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "migration-multi-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create multiple collections in old structure
	collections := []struct{ ns, name string }{
		{"ns1", "coll1"},
		{"ns1", "coll2"},
		{"ns2", "coll1"},
	}

	for _, c := range collections {
		oldDBPath := filepath.Join(tempDir, "collections", c.ns, c.name, "data.db")
		if err := os.MkdirAll(filepath.Dir(oldDBPath), 0755); err != nil {
			t.Fatal(err)
		}
		store, err := sqlite.NewSqliteStore(oldDBPath, collection.Options{})
		if err != nil {
			t.Fatal(err)
		}
		store.Close()
	}

	// Setup and run migration
	pathConfig := collection.NewPathConfig(tempDir)
	registryPath := pathConfig.RegistryDBPath()
	os.MkdirAll(filepath.Dir(registryPath), 0755)

	registryStore, err := collection.NewSqliteRegistryStore(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	defer registryStore.Close()

	migrator := collection.NewMigrator(pathConfig, registryStore)
	report, err := migrator.MigrateAll(context.Background())
	if err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Verify all migrated
	if report.Migrated != 3 {
		t.Errorf("Expected 3 migrated, got %d", report.Migrated)
	}

	// Verify each collection
	for _, c := range collections {
		newDBPath := pathConfig.CollectionDBPath(c.ns, c.name)
		if _, err := os.Stat(newDBPath); os.IsNotExist(err) {
			t.Errorf("Missing migrated DB: %s", newDBPath)
		}
	}
}

func TestMigration_Idempotent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "migration-idempotent-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create old structure
	oldDBPath := filepath.Join(tempDir, "collections", "ns1", "coll1", "data.db")
	os.MkdirAll(filepath.Dir(oldDBPath), 0755)
	store, _ := sqlite.NewSqliteStore(oldDBPath, collection.Options{})
	store.Close()

	// Setup
	pathConfig := collection.NewPathConfig(tempDir)
	registryPath := pathConfig.RegistryDBPath()
	os.MkdirAll(filepath.Dir(registryPath), 0755)
	registryStore, _ := collection.NewSqliteRegistryStore(registryPath)
	defer registryStore.Close()

	migrator := collection.NewMigrator(pathConfig, registryStore)

	// Run migration twice
	report1, err := migrator.MigrateAll(context.Background())
	if err != nil {
		t.Fatalf("First migration failed: %v", err)
	}

	report2, err := migrator.MigrateAll(context.Background())
	if err != nil {
		t.Fatalf("Second migration failed: %v", err)
	}

	// Second run should migrate 0 (already done)
	if report2.Migrated != 0 {
		t.Errorf("Second migration should migrate 0, got %d", report2.Migrated)
	}

	if report1.Migrated != 1 {
		t.Errorf("First migration should migrate 1, got %d", report1.Migrated)
	}
}

func TestMigration_NeedsMigration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "migration-check-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	pathConfig := collection.NewPathConfig(tempDir)
	registryPath := pathConfig.RegistryDBPath()
	os.MkdirAll(filepath.Dir(registryPath), 0755)
	registryStore, _ := collection.NewSqliteRegistryStore(registryPath)
	defer registryStore.Close()

	migrator := collection.NewMigrator(pathConfig, registryStore)

	// No old structure exists
	needsMigration, err := migrator.NeedsMigration(context.Background())
	if err != nil {
		t.Fatalf("NeedsMigration failed: %v", err)
	}
	if needsMigration {
		t.Errorf("Should not need migration when no old structure exists")
	}

	// Create old structure
	oldDBPath := filepath.Join(tempDir, "collections", "ns1", "coll1", "data.db")
	os.MkdirAll(filepath.Dir(oldDBPath), 0755)
	store, _ := sqlite.NewSqliteStore(oldDBPath, collection.Options{})
	store.Close()

	// Now it should need migration
	needsMigration, err = migrator.NeedsMigration(context.Background())
	if err != nil {
		t.Fatalf("NeedsMigration failed: %v", err)
	}
	if !needsMigration {
		t.Errorf("Should need migration when old structure exists")
	}
}
