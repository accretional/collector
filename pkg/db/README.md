# Database Package

The `db` package provides a database factory for creating storage backends that implement the `collection.Store` interface.

## Architecture

```
pkg/db/
├── store.go          # Factory function and config
├── README.md         # This file
└── sqlite/           # SQLite implementation
    ├── store.go      # SQLite store implementation
    ├── store_test.go # Store tests
    └── backup_test.go # Backup/availability tests
```

## Usage

```go
import (
    "github.com/accretional/collector/pkg/db"
    "github.com/accretional/collector/pkg/collection"
)

// Create a SQLite store
store, err := db.NewStore(ctx, db.Config{
    Type:       db.DBTypeSQLite,
    SQLitePath: "/path/to/database.db",
    Options: collection.Options{
        EnableJSON:   true,
        EnableFTS:    true,
        EnableVector: true,
        VectorDimensions: 384,
    },
})
```

## Supported Backends

| Backend    | Status      | Description |
|------------|-------------|-------------|
| `sqlite`   | ✅ Supported | SQLite with FTS5, JSON, and vector search |
| `postgres` | 🚧 Planned   | PostgreSQL with pgvector |

## Adding a New Backend

To add a new database backend (e.g., PostgreSQL):

### 1. Create the package

```
pkg/db/postgres/
├── store.go      # Implement collection.Store interface
└── store_test.go # Tests
```

### 2. Implement the Store interface

Your store must implement `collection.Store` from `pkg/collection/interfaces.go`:

```go
type Store interface {
    // CRUD operations
    CreateRecord(ctx context.Context, r *pb.CollectionRecord) error
    GetRecord(ctx context.Context, id string) (*pb.CollectionRecord, error)
    UpdateRecord(ctx context.Context, r *pb.CollectionRecord) error
    DeleteRecord(ctx context.Context, id string) error
    ListRecords(ctx context.Context, offset, limit int) ([]*pb.CollectionRecord, error)
    CountRecords(ctx context.Context) (int64, error)

    // Search
    Search(ctx context.Context, q *SearchQuery) ([]*SearchResult, error)

    // Maintenance
    Close() error
    Path() string
    Checkpoint(ctx context.Context) error
    Backup(ctx context.Context, destPath string) error
    BackupOnline(ctx context.Context, destPath string, pagesBatchSize int) error
    ReIndex(ctx context.Context) error
}
```

### 3. Add the factory case

Update `pkg/db/store.go`:

```go
const (
    DBTypeSQLite   DBType = "sqlite"
    DBTypePostgres DBType = "postgres" // Add new DB type
)

func NewStore(ctx context.Context, config Config) (collection.Store, error) {
    switch config.Type {
    case DBTypeSQLite:
        return newSQLiteStore(ctx, config)
    case DBTypePostgres:
        return newPostgresStore(ctx, config)  // Add this
    default:
        return nil, fmt.Errorf("unsupported database type: %s", config.Type)
    }
}

func newPostgresStore(ctx context.Context, config Config) (collection.Store, error) {
    return postgres.NewStore(
        config.PostgresHost,
        config.PostgresPort,
        // ... other config
        config.Options,
    )
}
```

### 4. Add config fields

Add any backend-specific configuration to `Config`:

```go
type Config struct {
    Type DBType

    // SQLite-specific
    SQLitePath string

    // PostgreSQL-specific
    PostgresHost     string
    PostgresPort     int
    PostgresUser     string
    PostgresPassword string
    PostgresDatabase string
    PostgresSSLMode  string

    // Common
    Options collection.Options
}
```

### 5. Write tests

Create tests that verify your implementation works correctly:

```go
// pkg/db/postgres/store_test.go
func TestStore_CreateAndGetRecord(t *testing.T) {
    // ...
}
```

## Build Requirements

The SQLite backend requires:
- CGO enabled (`CGO_ENABLED=1`)
- C compiler (gcc or clang)
- Build tag: `-tags sqlite_fts5`

```bash
go build -tags sqlite_fts5 ./...
go test -tags sqlite_fts5 ./pkg/db/...
```
