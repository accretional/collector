# Collection Package

The collection package provides a powerful ORM-like system for managing protobuf messages with full CRUD operations, search capabilities, and file attachments. It's the core data layer of the Collector system.

## Overview

Collections are like database tables for protobuf messages with:
- **Type-safe storage**: Any protobuf message type can be stored
- **Full-text search**: SQLite FTS5-powered search across message fields
- **JSON filtering**: Rich query capabilities using SQLite's JSON functions
- **File attachments**: Each record can have associated files in a hierarchical structure
- **gRPC API**: Complete CRUD + Search API for remote access
- **Custom methods**: Define custom RPC handlers for business logic

## Architecture

```
┌────────────────────────────────────────────────────┐
│              CollectionService                     │
│         (gRPC API Server)                          │
│                                                    │
│  CRUD: Create, Get, Update, Delete, List          │
│  Search: Full-text + JSON filtering               │
│  Custom: Invoke, Modify                           │
└─────────────────┬──────────────────────────────────┘
                  │
                  ▼
┌────────────────────────────────────────────────────┐
│              Collection                            │
│         (Core Data Structure)                      │
│                                                    │
│  • Message Type: protobuf message definition      │
│  • Store: SQLite backend with JSON + FTS          │
│  • FileSystem: Hierarchical file storage          │
└─────────────────┬──────────────────────────────────┘
                  │
         ┌────────┴────────┐
         │                 │
         ▼                 ▼
┌─────────────────┐  ┌──────────────────┐
│  SQLite Store   │  │   FileSystem     │
│                 │  │                  │
│  • Records      │  │  • Attachments   │
│  • JSON index   │  │  • Directories   │
│  • FTS5 index   │  │  • Metadata      │
└─────────────────┘  └──────────────────┘
```

## Core Concepts

### Collection

A Collection is a namespace-scoped container for protobuf messages of a specific type.

```go
type Collection struct {
    pb.Collection               // Embedded proto: namespace, name, message_type, etc.
    store         Store         // SQLite backend
    fs           FileSystem     // File storage
}
```

**Key Fields:**
- `Namespace`: Logical isolation (e.g., "production", "staging", "tenant-123")
- `Name`: Collection identifier (e.g., "users", "orders")
- `MessageType`: Reference to proto message type (`&pb.MessageTypeRef{Namespace: "myapp", MessageName: "User"}`)
- `IndexedFields`: Fields to index for fast queries
- `ServerEndpoint`: Optional gRPC endpoint for remote access

### CollectionRepo

A repository that manages multiple Collections. Provides discovery and routing.

```go
type CollectionRepo struct {
    collections map[string]*Collection  // namespace/name -> Collection
    store       Store
    mu          sync.RWMutex
}
```

**Capabilities:**
- Create new collections dynamically
- Discover collections by namespace, message type, or labels
- Route requests to appropriate collection
- Search across multiple collections

### CollectionService

gRPC service implementing the full CRUD + Search API:

```protobuf
service CollectionService {
  rpc Create(CreateRequest) returns (CreateResponse);
  rpc Get(GetRequest) returns (GetResponse);
  rpc Update(UpdateRequest) returns (UpdateResponse);
  rpc Delete(DeleteRequest) returns (DeleteResponse);
  rpc List(ListRequest) returns (ListResponse);
  rpc Search(SearchResponse) returns (SearchResponse);
  rpc Batch(BatchRequest) returns (BatchResponse);
  rpc Describe(DescribeRequest) returns (DescribeResponse);
  rpc Modify(ModifyRequest) returns (ModifyResponse);
  rpc Meta(MetaRequest) returns (MetaResponse);
  rpc Invoke(InvokeRequest) returns (InvokeResponse);
}
```

## Usage Examples

### Creating a Collection

```go
import (
    "github.com/accretional/collector/pkg/collection"
    "github.com/accretional/collector/pkg/db"
    pb "github.com/accretional/collector/gen/collector"
)

// Create SQLite store
store, err := db.NewStore(ctx, db.Config{
	Type:       db.DBTypeSQLite,
    SQLitePath: "./data/users.db",
    Options: collection.Options{
        EnableJSON: true,  // Enable JSONB indexing
        EnableFTS:  true,  // Enable full-text search
    },
})

// Create collection
coll, err := collection.NewCollection(
    &pb.Collection{
        Namespace: "production",
        Name:      "users",
        MessageType: &pb.MessageTypeRef{
            Namespace:   "myapp",
            MessageName: "User",
        },
        IndexedFields: []string{"email", "username"},
    },
    store,
    &collection.LocalFileSystem{BaseDir: "./data/files"},
)
```

### CRUD Operations

```go
// CREATE - wrap your proto in a CollectionRecord
user := &pb.User{
    Id:    "user-123",
    Name:  "Alice",
    Email: "alice@example.com",
}
protoData, _ := proto.Marshal(user)

err := coll.CreateRecord(ctx, &pb.CollectionRecord{
    Id:        "user-123",
    ProtoData: protoData,
    Metadata: &pb.Metadata{
        Labels: map[string]string{"role": "admin"},
    },
})

// GET - returns CollectionRecord, unmarshal ProtoData
record, err := coll.GetRecord(ctx, "user-123")
user := &pb.User{}
proto.Unmarshal(record.ProtoData, user)

// UPDATE - pass full CollectionRecord
user.Name = "Alice Smith"
protoData, _ = proto.Marshal(user)
err = coll.UpdateRecord(ctx, &pb.CollectionRecord{
    Id:        "user-123",
    ProtoData: protoData,
})

// DELETE
err = coll.DeleteRecord(ctx, "user-123")

// LIST
records, err := coll.ListRecords(ctx, 10, 0)  // limit=10, offset=0
```

### Search with Full-Text and Filters

```go
// Full-text search
results, err := coll.SearchRecords(ctx, &pb.SearchRequest{
    Query: "alice engineer",  // FTS5 query
    Limit: 10,
})

// JSON filtering
results, err := coll.SearchRecords(ctx, &pb.SearchRequest{
    Filters: []*pb.SearchFilter{
        {
            Field:    "metadata.role",
            Operator: pb.SearchOperator_EQUALS,
            Value:    "admin",
        },
        {
            Field:    "age",
            Operator: pb.SearchOperator_GREATER_THAN,
            Value:    "25",
        },
    },
    Limit: 10,
})

// Combined search
results, err := coll.SearchRecords(ctx, &pb.SearchRequest{
    Query: "engineer",
    Filters: []*pb.SearchFilter{
        {Field: "status", Operator: pb.SearchOperator_EQUALS, Value: "active"},
    },
    OrderBy: "created_at",
    Desc:    true,
    Limit:   20,
})
```

### File Attachments

```go
// Save a file with a record
fileData := []byte("user profile picture")
err := coll.SaveFile(ctx, "user-123/profile.jpg", fileData)

// Get file
data, err := coll.GetFile(ctx, "user-123/profile.jpg")

// List files for a record
files, err := coll.ListFiles(ctx, "user-123")

// Delete file
err := coll.DeleteFile(ctx, "user-123/profile.jpg")

// Save entire directory
err := coll.SaveDir(ctx, "user-123/docs", "./local-docs")
```

## CollectionRepo - Multi-Collection Management

### Creating Collections

```go
repo := collection.NewCollectionRepo(store)

// Create a new collection
resp, err := repo.CreateCollection(ctx, &pb.CreateCollectionRequest{
    Collection: &pb.Collection{
        Namespace:   "production",
        Name:        "orders",
        MessageType: "collector.Order",
    },
})
```

### Discovery

```go
// Discover all collections in a namespace
resp, err := repo.Discover(ctx, &pb.DiscoverRequest{
    Namespace: "production",
})

// Discover by message type
resp, err := repo.Discover(ctx, &pb.DiscoverRequest{
    MessageType: "collector.User",
})

// Discover with labels
resp, err := repo.Discover(ctx, &pb.DiscoverRequest{
    Labels: map[string]string{"env": "prod", "region": "us-west"},
})
```

### Routing

```go
// Route request to appropriate collection
resp, err := repo.Route(ctx, &pb.RouteRequest{
    Collection: &pb.Collection{
        Namespace: "production",
        Name:      "users",
    },
})
// Returns server_endpoint for the collection
```

### Cross-Collection Search

```go
// Search across multiple collections
resp, err := repo.SearchCollections(ctx, &pb.SearchCollectionsRequest{
    Namespace: "production",
    Query:     "alice",
    Collections: []string{"users", "profiles"},  // Optional filter
})
```

## gRPC API Server

### Setting Up CollectionService

```go
import (
    "net"
    "google.golang.org/grpc"
    pb "github.com/accretional/collector/gen/collector"
)

// Create repository
repo := collection.NewCollectionRepo(store)

// Create gRPC server
server := collection.NewCollectionServer(repo)

// Start gRPC service
lis, _ := net.Listen("tcp", ":50051")
grpcServer := grpc.NewServer()
pb.RegisterCollectionServiceServer(grpcServer, server)
grpcServer.Serve(lis)
```

### Client Usage

```go
// Connect to CollectionService
conn, _ := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
client := pb.NewCollectionServiceClient(conn)

// Create record
resp, err := client.Create(ctx, &pb.CreateRequest{
    Collection: &pb.Collection{Namespace: "production", Name: "users"},
    Record:     &pb.Record{Id: "user-123", Data: userProto},
})

// Search
resp, err := client.Search(ctx, &pb.SearchRequest{
    Collection: &pb.Collection{Namespace: "production", Name: "users"},
    Query:      "alice engineer",
    Limit:      10,
})
```

## Search Capabilities

### Full-Text Search (FTS5)

SQLite FTS5 powers full-text search:

```go
// Simple text search
query := "distributed systems"

// Boolean operators
query := "alice AND engineer"
query := "alice OR bob"
query := "alice NOT manager"

// Phrase search
query := `"senior engineer"`

// Prefix matching
query := "eng*"  // Matches "engineer", "engineering", etc.
```

### JSON Filtering

Rich filtering on JSON-serialized protobuf fields:

```go
filters := []*pb.SearchFilter{
    // Equality
    {Field: "status", Operator: pb.SearchOperator_EQUALS, Value: "active"},

    // Comparison
    {Field: "age", Operator: pb.SearchOperator_GREATER_THAN, Value: "25"},
    {Field: "age", Operator: pb.SearchOperator_LESS_THAN_OR_EQUAL, Value: "65"},

    // Nested fields
    {Field: "metadata.role", Operator: pb.SearchOperator_EQUALS, Value: "admin"},
    {Field: "address.city", Operator: pb.SearchOperator_EQUALS, Value: "SF"},

    // JSON contains (for arrays/objects)
    {Field: "tags", Operator: pb.SearchOperator_CONTAINS, Value: `"engineer"`},

    // Field existence
    {Field: "premium_until", Operator: pb.SearchOperator_EXISTS},
}
```

### Combined Queries

```go
// Full-text + filters + ordering + pagination
resp, err := client.Search(ctx, &pb.SearchRequest{
    Collection: &pb.Collection{Namespace: "production", Name: "users"},
    Query:      "senior engineer",
    Filters: []*pb.SearchFilter{
        {Field: "status", Operator: pb.SearchOperator_EQUALS, Value: "active"},
        {Field: "years_exp", Operator: pb.SearchOperator_GREATER_THAN, Value: "5"},
    },
    OrderBy: "created_at",
    Desc:    true,
    Limit:   20,
    Offset:  0,
})
```

## Advanced Features

### Custom Handlers

Register custom RPC handlers for business logic:

```go
coll.RegisterCustomHandler("CalculateStats", func(ctx context.Context, input *pb.Record) (*pb.Record, error) {
    // Custom logic here
    stats := calculateUserStats(input)
    return stats, nil
})

// Invoke via API
resp, err := client.Invoke(ctx, &pb.InvokeRequest{
    Collection: &pb.Collection{Namespace: "production", Name: "users"},
    Method:     "CalculateStats",
    Input:      userRecord,
})
```

### Batch Operations

```go
// Batch create/update/delete
resp, err := client.Batch(ctx, &pb.BatchRequest{
    Collection: &pb.Collection{Namespace: "production", Name: "users"},
    Operations: []*pb.BatchOperation{
        {Type: pb.BatchOperationType_CREATE, Record: user1},
        {Type: pb.BatchOperationType_CREATE, Record: user2},
        {Type: pb.BatchOperationType_UPDATE, Record: user3},
        {Type: pb.BatchOperationType_DELETE, RecordId: "user-old"},
    },
})
```

### Metadata

```go
// Get collection metadata
resp, err := client.Meta(ctx, &pb.MetaRequest{
    Collection: &pb.Collection{Namespace: "production", Name: "users"},
})

// Returns: record_count, size_bytes, indexed_fields, etc.
```

### Schema Introspection

```go
// Describe collection schema
resp, err := client.Describe(ctx, &pb.DescribeRequest{
    Collection: &pb.Collection{Namespace: "production", Name: "users"},
})

// Returns: message_type, fields, indexes, capabilities
```

## Data Model

### Record Storage

Records are stored with a dual representation for efficient storage and searching:

1. **`proto_data` (BLOB)**: Binary protobuf - the source of truth
2. **`jsontext` (TEXT)**: JSON derived from proto_data for search indexing
3. **`labels` (TEXT)**: JSON object for label-based filtering
4. **FTS tokens**: For full-text search (when EnableFTS is true)

```sql
CREATE TABLE records (
    id TEXT PRIMARY KEY,
    proto_data BLOB,     -- Binary protobuf (source of truth)
    data_uri TEXT,       -- Optional reference to external data
    created_at INTEGER,
    updated_at INTEGER,
    labels TEXT          -- JSON: {"key": "value", ...}
);

-- Added when EnableJSON is true:
ALTER TABLE records ADD COLUMN jsontext TEXT;  -- JSON derived from proto_data
```

**JSON Conversion Pipeline:**
- When a record is created/updated, the store converts `proto_data` to JSON for the `jsontext` column
- Conversion uses `ProtoToJSONConverter` callbacks (system types use static converters, user types use registry-based dynamic converters)
- If no converter is available and `proto_data` isn't valid JSON, falls back to `"{}"` (record stored but JSON fields not searchable)
- `proto_data` remains the source of truth; `jsontext` is purely for search indexing

### File Storage

Files stored in hierarchical directory structure:

```
./data/files/
  production/
    users/
      user-123/
        profile.jpg
        documents/
          resume.pdf
          cover-letter.pdf
      user-456/
        avatar.png
```

## Testing

Comprehensive test coverage:

```bash
# Run all collection tests
go test ./pkg/collection/... -v

# Run specific test suites
go test ./pkg/collection -run TestCollectionServer
go test ./pkg/collection -run TestSearch
go test ./pkg/collection -run TestDurability
```

**Test Coverage:**
- CRUD operations (15+ tests)
- Search (full-text, JSONB, combined - 12+ tests)
- File operations (save, get, delete, list - 8+ tests)
- CollectionRepo (create, discover, route, search - 15+ tests)
- gRPC server (all RPC methods - 16+ tests)
- Durability (concurrent access, recovery, atomicity - 12+ tests)
- Performance (stress tests for large datasets)

## SQLite Backend

### Store Interface

```go
type Store interface {
    CreateRecord(ctx context.Context, id string, data []byte) error
    GetRecord(ctx context.Context, id string) ([]byte, error)
    UpdateRecord(ctx context.Context, id string, data []byte) error
    DeleteRecord(ctx context.Context, id string) error
    ListRecords(ctx context.Context, limit, offset int) ([]Record, error)
    SearchRecords(ctx context.Context, req *pb.SearchRequest) ([]Record, error)
    Close() error
}
```

### SQLite Features

- **WAL mode**: Better concurrency for reads/writes
- **JSON functions**: `json_extract()` and `json_each()` for filtering
- **FTS5**: Full-text search with ranking
- **Transactions**: ACID guarantees for all operations
- **Connection pooling**: Efficient resource usage

### Configuration

```go
options := collection.Options{
    EnableJSON: true,   // Enable JSON indexing (adds jsontext column)
    EnableFTS:  true,   // Enable full-text search
}

store, err := db.NewStore(ctx, db.Config{
    Type: db.DBTypeSQLite,
    SQLitePath: dbPath,
    Options:    options,
})

// For JSON search to work properly, set a converter for the collection's type:
// System types have built-in converters
store.SetJSONConverter(collection.GetSystemTypeConverter("Collection"))

// Or use the registry-based factory for dynamic type lookup
converterFactory := collection.NewRegistryConverterFactory(lookupFunc)
```

## Performance Considerations

- **Indexed fields**: Specify fields for fast lookups
- **Batch operations**: Use batch API for bulk operations
- **Pagination**: Always use limit/offset for large result sets
- **Connection pooling**: SQLite handles concurrent reads efficiently
- **WAL mode**: Enables concurrent reads during writes
- **FTS optimization**: Full-text search scales to millions of records

## Best Practices

1. **Use namespaces**: Isolate data by tenant, environment, or feature
2. **Index strategically**: Only index fields you query frequently
3. **Enable FTS selectively**: Only for collections needing text search
4. **Handle errors**: Check all operation results
5. **Use transactions**: For multi-record operations
6. **Monitor size**: Track collection metadata regularly
7. **Test search queries**: FTS5 syntax can be tricky
8. **Version schemas**: Include version in message_type for migrations

## Integration with Registry

Collections validate against the Registry when validation is enabled:

```go
import "github.com/accretional/collector/pkg/registry"

// Create gRPC server with validation
grpcServer := registry.NewServerWithValidation(registryServer, namespace)

// Register CollectionService
collectionServer := collection.NewCollectionServer(repo)
pb.RegisterCollectionServiceServer(grpcServer, collectionServer)

// Now all RPCs are validated against registry before execution
```

## Future Enhancements

- Query optimizer for complex searches
- Replication across multiple collectors
- Streaming APIs for large result sets
- Schema evolution and migrations
- Computed/virtual fields
- Triggers and hooks on CRUD operations
- Time-series optimizations

## Logging & Observability

The system uses a structured logging system backed by a dedicated collection (`system/logs`).

### System Logger

`SystemLogger` provides structured logging that writes to both stdout (for immediate operator visibility) and persists to the `system/logs` collection (for queryability and retention).

```go
// Usage
log := collection.NewSystemLogger(logsCollection)
log.Info("Backup started", "collection", "users", "size_bytes", 1024)
log.Error("Failed to connect", "error", err)
```

**Features:**
- **Buffered writes**: Writes to SQLite are buffered to prevent blocking critical paths.
- **Persistence**: Logs are stored as protobuf records in `system/logs`.
- **Searchable**: Search logs by component and level via `Metadata.Labels` (e.g., `labels.level = "ERROR"`).
- **Standard levels**: DEBUG, INFO, WARN, ERROR.

**Note**: Full-text search on log messages is not currently supported because log payloads are stored as binary protobufs to maximize performance. Use `component` and `level` labels for filtering.

