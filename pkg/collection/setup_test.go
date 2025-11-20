package collection_test

import (
	"os"
	"path/filepath"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
)

// setupTestCollection creates a real SQLite-backed collection for integration testing
func setupTestCollection(t *testing.T) (*collection.Collection, func()) {
	t.Helper()

	// 1. Temp Dir
	tempDir, err := os.MkdirTemp("", "coll-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// 2. Setup Store
	dbPath := filepath.Join(tempDir, "test.db")
	store, err := sqlite.NewSqliteStore(dbPath, collection.Options{
		EnableFTS:  true,
		EnableJSON: true,
	})
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create store: %v", err)
	}

	// 3. Setup FS
	fs := &collection.LocalFileSystem{Root: filepath.Join(tempDir, "files")}

	// 4. Setup Collection
	proto := &pb.Collection{
		Namespace: "test",
		Name:      "unit-test",
	}
	
	coll, err := collection.NewCollection(proto, store, fs)
	if err != nil {
		store.Close()
		os.RemoveAll(tempDir)
		t.Fatalf("failed to create collection: %v", err)
	}

	cleanup := func() {
		coll.Close() // Closes store
		os.RemoveAll(tempDir)
	}

	return coll, cleanup
}
