package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	basePath := "./data/collections"

	// 1. Setup Namespace/Name
	namespace := "demo"
	name := "tasks"
	fullPath := filepath.Join(basePath, namespace, name)

	if err := os.MkdirAll(fullPath, 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	// 2. Initialize Metadata
	proto := &pb.Collection{
		Namespace: namespace,
		Name:      name,
		Metadata: &pb.Metadata{
			Labels: map[string]string{"version": "2.0-refactor"},
		},
	}

	// 3. Initialize Dependencies (The "Glue")

	// A. SQLite Store
	dbPath := filepath.Join(fullPath, "data.db")
	storeOpts := collection.Options{
		EnableFTS:  true,
		EnableJSON: true,
	}
	store, err := sqlite.NewSqliteStore(dbPath, storeOpts)
	if err != nil {
		return fmt.Errorf("init sqlite: %w", err)
	}
	defer store.Close()

	// B. Local Filesystem
	// (We need a concrete implementation of collection.FileSystem)
	// For this example, we use a simple wrapper around os methods.
	fs, err := collection.NewLocalFileSystem(filepath.Join(fullPath, "files"))
	if err != nil {
		log.Fatalf("Failed to create filesystem: %v", err)
	}

	// 4. Create Domain Object
	coll, err := collection.NewCollection(proto, store, fs)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("✓ Collection Ready: %s/%s\n", coll.GetNamespace(), coll.GetName())

	// Test a create
	err = coll.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        "init-001",
		ProtoData: []byte(`{"msg": "It works!"}`),
	})
	if err != nil {
		return err
	}
	fmt.Println("✓ Created test record")

	return nil
}
