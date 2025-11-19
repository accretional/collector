package collection

import (
    "database/sql"
    "fmt"
    "os"
    "path/filepath"
    "time"

    pb "github.com/accretional/collector/gen/collector"
    _ "modernc.org/sqlite"
)

// Collection wraps the proto message and manages persistence
type Collection struct {
    proto      *pb.Collection
    db         *sql.DB
    basePath   string
    dbPath     string
    filesPath  string
}

// CollectionOptions configures collection creation/loading
type CollectionOptions struct {
    BasePath string // Root directory for all collections
}

// NewCollection creates a new Collection and initializes storage
func NewCollection(proto *pb.Collection, opts CollectionOptions) (*Collection, error) {
    if proto.Namespace == "" || proto.Name == "" {
        return nil, fmt.Errorf("namespace and name are required")
    }

    // Setup paths: basePath/namespace/name/
    collectionPath := filepath.Join(opts.BasePath, proto.Namespace, proto.Name)
    dbPath := filepath.Join(collectionPath, "data.db")
    filesPath := filepath.Join(collectionPath, "files")

    c := &Collection{
        proto:     proto,
        basePath:  opts.BasePath,
        dbPath:    dbPath,
        filesPath: filesPath,
    }

    // Create directory structure
    if err := os.MkdirAll(collectionPath, 0755); err != nil {
        return nil, fmt.Errorf("failed to create collection directory: %w", err)
    }

    if err := os.MkdirAll(filesPath, 0755); err != nil {
        return nil, fmt.Errorf("failed to create files directory: %w", err)
    }

    // Initialize SQLite database
    if err := c.initDatabase(); err != nil {
        return nil, fmt.Errorf("failed to initialize database: %w", err)
    }

    // Set metadata timestamps
    now := time.Now()
    if proto.Metadata == nil {
        proto.Metadata = &pb.Metadata{
            CreatedAt: timestampProto(now),
            UpdatedAt: timestampProto(now),
        }
    }

    // Save the collection metadata
    if err := c.saveMetadata(); err != nil {
        return nil, fmt.Errorf("failed to save metadata: %w", err)
    }

    return c, nil
}

// LoadCollection loads an existing collection from storage
func LoadCollection(namespace, name string, opts CollectionOptions) (*Collection, error) {
    collectionPath := filepath.Join(opts.BasePath, namespace, name)
    dbPath := filepath.Join(collectionPath, "data.db")
    filesPath := filepath.Join(collectionPath, "files")

    // Check if collection exists
    if _, err := os.Stat(dbPath); os.IsNotExist(err) {
        return nil, fmt.Errorf("collection %s/%s does not exist", namespace, name)
    }

    c := &Collection{
        basePath:  opts.BasePath,
        dbPath:    dbPath,
        filesPath: filesPath,
    }

    // Open database
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, fmt.Errorf("failed to open database: %w", err)
    }
    c.db = db

    // Load metadata
    proto, err := c.loadMetadata()
    if err != nil {
        return nil, fmt.Errorf("failed to load metadata: %w", err)
    }
    c.proto = proto

    // Load filesystem structure if it exists
    if err := c.loadFilesystem(); err != nil {
        return nil, fmt.Errorf("failed to load filesystem: %w", err)
    }

    return c, nil
}

// ToProto returns the current state as a proto message
func (c *Collection) ToProto(includeRecords bool) (*pb.Collection, error) {
    proto := &pb.Collection{
        Namespace:      c.proto.Namespace,
        Name:           c.proto.Name,
        MessageType:    c.proto.MessageType,
        IndexedFields:  c.proto.IndexedFields,
        ServerEndpoint: c.proto.ServerEndpoint,
        Metadata:       c.proto.Metadata,
    }

    if includeRecords {
        records, err := c.ListRecords(0, 1000) // Default limit
        if err != nil {
            return nil, fmt.Errorf("failed to list records: %w", err)
        }
        // Note: We don't populate records in the proto directly,
        // they're accessed via storage methods
    }

    // Filesystem structure is loaded on-demand
    if c.proto.Dir != nil {
        proto.Dir = c.proto.Dir
    }

    return proto, nil
}

// Close closes the database connection
func (c *Collection) Close() error {
    if c.db != nil {
        return c.db.Close()
    }
    return nil
}

// GetPath returns the collection's filesystem path
func (c *Collection) GetPath() string {
    return filepath.Join(c.basePath, c.proto.Namespace, c.proto.Name)
}

// GetNamespace returns the namespace
func (c *Collection) GetNamespace() string {
    return c.proto.Namespace
}

// GetName returns the name
func (c *Collection) GetName() string {
    return c.proto.Name
}

// Helper to convert time.Time to protobuf Timestamp
func timestampProto(t time.Time) *pb.Timestamp {
    return &pb.Timestamp{
        Seconds: t.Unix(),
        Nanos:   int32(t.Nanosecond()),
    }
}

// Helper to convert protobuf Timestamp to time.Time
func timeFromProto(ts *pb.Timestamp) time.Time {
    if ts == nil {
        return time.Time{}
    }
    return time.Unix(ts.Seconds, int64(ts.Nanos))
}
