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
    // Setup base path for collections
    basePath := "./data/collections"
    if err := os.MkdirAll(basePath, 0755); err != nil {
        return fmt.Errorf("failed to create base path: %w", err)
    }

    opts := collection.CollectionOptions{
        BasePath: basePath,
    }

    // Example 1: Create a new collection
    fmt.Println("=== Creating new collection ===")
    proto := &pb.Collection{
        Namespace: "examples",
        Name:      "users",
        MessageType: &pb.MessageTypeRef{
            Namespace:   "examples",
            MessageName: "User",
        },
        IndexedFields: []string{"email", "username"},
        Metadata: &pb.Metadata{
            Labels: map[string]string{
                "environment": "development",
                "version":     "1.0",
            },
        },
    }

    coll, err := collection.NewCollection(proto, opts)
    if err != nil {
        return fmt.Errorf("failed to create collection: %w", err)
    }
    defer coll.Close()

    fmt.Printf("Created collection: %s/%s at %s\n", 
        coll.GetNamespace(), coll.GetName(), coll.GetPath())

    // Example 2: Add some records
    fmt.Println("\n=== Adding records ===")
    records := []*pb.CollectionRecord{
        {
            Id:        "user-001",
            ProtoData: []byte(`{"name": "Alice", "email": "alice@example.com"}`),
            Metadata: &pb.Metadata{
                Labels: map[string]string{"role": "admin"},
            },
        },
        {
            Id:        "user-002",
            ProtoData: []byte(`{"name": "Bob", "email": "bob@example.com"}`),
            Metadata: &pb.Metadata{
                Labels: map[string]string{"role": "user"},
            },
        },
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            return fmt.Errorf("failed to create record: %w", err)
        }
        fmt.Printf("Created record: %s\n", record.Id)
    }

    // Example 3: Add some files
    fmt.Println("\n=== Adding files ===")
    files := []*pb.CollectionData{
        {
            Name: "README.md",
            Content: &pb.CollectionData_Data{
                Data: []byte("# Users Collection\n\nThis collection stores user data."),
            },
        },
        {
            Name: "config.json",
            Content: &pb.CollectionData_Data{
                Data: []byte(`{"version": "1.0", "schema": "user"}`),
            },
        },
    }

    for _, file := range files {
        if err := coll.SaveFile(file.Name, file); err != nil {
            return fmt.Errorf("failed to save file: %w", err)
        }
        fmt.Printf("Saved file: %s\n", file.Name)
    }

    // Example 4: Retrieve and display records
    fmt.Println("\n=== Listing records ===")
    allRecords, err := coll.ListRecords(0, 10)
    if err != nil {
        return fmt.Errorf("failed to list records: %w", err)
    }

    for _, record := range allRecords {
        fmt.Printf("ID: %s, Data: %s, Labels: %v\n",
            record.Id,
            string(record.ProtoData),
            record.Metadata.Labels,
        )
    }

    count, _ := coll.CountRecords()
    fmt.Printf("Total records: %d\n", count)

    // Example 5: Retrieve a specific record
    fmt.Println("\n=== Getting specific record ===")
    record, err := coll.GetRecord("user-001")
    if err != nil {
        return fmt.Errorf("failed to get record: %w", err)
    }
    fmt.Printf("Retrieved: %s -> %s\n", record.Id, string(record.ProtoData))

    // Example 6: Close and reload the collection
    fmt.Println("\n=== Reloading collection from disk ===")
    coll.Close()

    reloaded, err := collection.LoadCollection("examples", "users", opts)
    if err != nil {
        return fmt.Errorf("failed to reload collection: %w", err)
    }
    defer reloaded.Close()

    fmt.Printf("Reloaded collection: %s/%s\n",
        reloaded.GetNamespace(), reloaded.GetName())

    reloadedProto, err := reloaded.ToProto(false)
    if err != nil {
        return fmt.Errorf("failed to convert to proto: %w", err)
    }

    fmt.Printf("Message type: %s/%s\n",
        reloadedProto.MessageType.Namespace,
        reloadedProto.MessageType.MessageName)
    fmt.Printf("Indexed fields: %v\n", reloadedProto.IndexedFields)
    fmt.Printf("Labels: %v\n", reloadedProto.Metadata.Labels)

    // Example 7: List files
    fmt.Println("\n=== Listing files ===")
    fileList, err := reloaded.ListFiles()
    if err != nil {
        return fmt.Errorf("failed to list files: %w", err)
    }
    for _, filePath := range fileList {
        fmt.Printf("File: %s\n", filePath)
    }

    fmt.Println("\n=== Complete! ===")
    return nil
}
