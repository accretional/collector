package collection_test

import (
	"os"
	"path/filepath"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
)

// setupTestCollection creates a REAL SQLite-backed collection for integration testing.
func setupTestCollection(t *testing.T) (*collection.Collection, func()) {
	t.Helper()

	// 1. Create a temporary directory for this test run
	tempDir, err := os.MkdirTemp("", "coll-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// 2. Create PathConfig
	pathConfig := collection.NewPathConfig(tempDir)

	namespace := "test-ns"
	name := "test-collection"

	// 3. Initialize the REAL SQLite Store
	dbPath := pathConfig.CollectionDBPath(namespace, name)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatalf("failed to create db dir: %v", err)
	}

	store, err := sqlite.NewSqliteStore(dbPath, collection.Options{
		EnableFTS:  true, // Test FTS tables
		EnableJSON: true, // Test JSON columns
	})
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create sqlite store: %v", err)
	}

	// 4. Initialize the REAL Local Filesystem
	filesPath := pathConfig.CollectionFilesPath(namespace, name)
	fs, err := collection.NewLocalFileSystem(filesPath)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create filesystem: %v", err)
	}

	// 5. Create the Collection Domain Object
	proto := &pb.Collection{
		Namespace: namespace,
		Name:      name,
		Metadata:  &pb.Metadata{},
	}

	coll, err := collection.NewCollection(proto, store, fs)
	if err != nil {
		store.Close()
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create collection: %v", err)
	}

	// Cleanup function to remove DB and files after test
	cleanup := func() {
		coll.Close() // Closes SQLite connection
		os.RemoveAll(tempDir)
	}

	return coll, cleanup
}

// setupTestRepo creates a REAL CollectionRepo for integration testing.
func setupTestRepo(t *testing.T) (collection.CollectionRepo, func()) {
	t.Helper()

	// 1. Create a temporary directory for this test run
	tempDir, err := os.MkdirTemp("", "repo-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// 2. Create PathConfig
	pathConfig := collection.NewPathConfig(tempDir)

	// 3. Create registry store
	registryPath := pathConfig.RegistryDBPath()
	if err := os.MkdirAll(filepath.Dir(registryPath), 0755); err != nil {
		t.Fatalf("failed to create registry dir: %v", err)
	}

	registryStore, err := collection.NewSqliteRegistryStore(registryPath)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create registry store: %v", err)
	}

	// 4. Create dummy store (not used for metadata anymore)
	dummyStore, err := sqlite.NewSqliteStore(":memory:", collection.Options{})
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create dummy store: %v", err)
	}

	// 5. Create the DefaultCollectionRepo with StoreFactory
	storeFactory := func(path string, opts collection.Options) (collection.Store, error) {
		return sqlite.NewSqliteStore(path, opts)
	}
	repo := collection.NewCollectionRepo(dummyStore, pathConfig, registryStore, storeFactory)

	// Cleanup function
	cleanup := func() {
		registryStore.Close()
		dummyStore.Close()
		os.RemoveAll(tempDir)
	}

	return repo, cleanup
}
