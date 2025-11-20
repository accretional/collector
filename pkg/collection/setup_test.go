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

	// 2. Initialize the REAL SQLite Store
	dbPath := filepath.Join(tempDir, "test.db")
	
	store, err := sqlite.NewSqliteStore(dbPath, collection.Options{
		EnableFTS:  true,  // Test FTS tables
		EnableJSON: true,  // Test JSON columns
	})
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create sqlite store: %v", err)
	}

	// 3. Initialize the REAL Local Filesystem
	fs := &collection.LocalFileSystem{
		Root: filepath.Join(tempDir, "files"),
	}

	// 4. Create the Collection Domain Object
	proto := &pb.Collection{
		Namespace: "test-ns",
		Name:      "test-collection",
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
