package main

import (
    "fmt"
    "log"
    "os"

    pb "github.com/accretional/collector/gen/collector"
    "github.com/accretional/collector/pkg/collection"
)

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}

func run() error {
    // Setup
    basePath := "./data/collections"
    if err := os.MkdirAll(basePath, 0755); err != nil {
        return fmt.Errorf("failed to create base path: %w", err)
    }

    opts := collection.CollectionOptions{
        BasePath: basePath,
    }

    // Create a simple collection
    proto := &pb.Collection{
        Namespace: "demo",
        Name:      "tasks",
        MessageType: &pb.MessageTypeRef{
            Namespace:   "demo",
            MessageName: "Task",
        },
        Metadata: &pb.Metadata{
            Labels: map[string]string{
                "version": "1.0",
            },
        },
    }

    coll, err := collection.NewCollection(proto, opts)
    if err != nil {
        return fmt.Errorf("failed to create collection: %w", err)
    }
    defer coll.Close()

    fmt.Printf("✓ Created collection: %s/%s\n", coll.GetNamespace(), coll.GetName())
    fmt.Printf("✓ Path: %s\n", coll.GetPath())
    fmt.Println("\nRun 'go test -v ./pkg/collection/...' to see comprehensive tests")

    return nil
}
