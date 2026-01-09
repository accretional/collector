package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()

	// 1. Setup Path Configuration
	pathConfig := collection.NewPathConfig("./data")

	// 2. Setup Namespace/Name
	namespace := "demo"
	name := "tasks"

	// 3. Initialize Metadata
	proto := &pb.Collection{
		Namespace: namespace,
		Name:      name,
		Metadata: &pb.Metadata{
			Labels: map[string]string{"version": "2.0-refactor"},
		},
	}

	// 4. Initialize Dependencies (The "Glue")

	// Determine whether to enable vector support via env toggle.
	vectorSearchEnabled := os.Getenv("ENABLE_VECTOR_SEARCH") != ""
	vectorDims := 128
	if v := os.Getenv("VECTOR_DIMENSIONS"); v != "" {
		if d, err := strconv.Atoi(v); err == nil {
			vectorDims = d
		} else {
			return fmt.Errorf("invalid VECTOR_DIMENSIONS: %w", err)
		}
	}

	// A. SQLite Store
	dbPath, err := pathConfig.CollectionDBPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid collection path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	collectionCfg := &pb.CollectionConfig{
		Search: &pb.SearchConfig{
			EnableFts:          true,
			EnableJson:         true,
			EnableVectorSearch: vectorSearchEnabled,
		},
	}
	if vectorSearchEnabled {
		collectionCfg.Vector = &pb.VectorConfig{
			Dimensions:   int32(vectorDims),
			EmbedderType: "deterministic",
		}
	}

	store, err := db.NewStore(ctx, db.Config{
		Type:             db.DBTypeSQLite,
		SQLitePath:       dbPath,
		CollectionConfig: collectionCfg,
	})
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer store.Close()

	// B. Local Filesystem
	filesPath, err := pathConfig.CollectionFilesPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid collection files path: %w", err)
	}
	fs, err := collection.NewLocalFileSystem(filesPath)
	if err != nil {
		log.Fatalf("Failed to create filesystem: %v", err)
	}

	// 5. Create Domain Object
	coll, err := collection.NewCollection(proto, store, fs)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("✓ Collection Ready: %s/%s\n", coll.GetNamespace(), coll.GetName())
	fmt.Printf("  Database: %s\n", dbPath)
	fmt.Printf("  Files: %s\n", filesPath)

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
