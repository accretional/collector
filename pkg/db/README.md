# Database Package

The `db` package provides a factory pattern for creating database stores with support for multiple databases, extension loading and capability detection.

## Overview

The database package abstracts database creation behind a unified factory interface (`db.NewStore()`), hiding implementation details from higher layers. This architecture makes it easy to add new database backends in the future.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│              Application Layer                      │
│  (Collection, CollectionRepo, etc.)                 │
└─────────────────┬───────────────────────────────────┘
                  │
                  │ Uses: db.NewStore()
                  ▼
┌─────────────────────────────────────────────────────┐
│              db Package (Factory)                   │
│                                                     │
│  • StoreConfig - Configuration for store creation   │
│  • ExtensionConfig - Extension loading config       │
│  • Capability constants (vector, fts, json)         │
│  • Error types (ExtensionError, CapabilityError)    │
└─────────────────┬───────────────────────────────────┘
                  │
                  │ Creates:
                  ▼
         ┌────────┴────────┐
         │                 │
         ▼                 ▼
┌─────────────────┐  ┌──────────────────┐
│  SQLite Store   │  │  (Future types)  │
│                 │  │                  │
│  • Pure Go      │  │  • PostgreSQL    │
│  • CGo + Exts   │  │  • MySQL         │
│                 │  │  • etc.          │
└─────────────────┘  └──────────────────┘
```

## Core Concepts

### Factory Pattern

All database stores are created through the `db.NewStore()` factory function:

```go
import (
    "context"
    "github.com/accretional/collector/pkg/db"
    "github.com/accretional/collector/pkg/collection"
)

ctx := context.Background()

store, err := db.NewStore(ctx, db.StoreConfig{
    Type: "sqlite",  // Default, can be omitted
    Path: "./data.db",
    Options: collection.Options{
        EnableFTS:  true,
        EnableJSON: true,
    },
})
if err != nil {
    return err
}
defer store.Close()
```

### Store Configuration

`StoreConfig` allows you to configure:

- **Type**: Database type (Default: `"sqlite"`)
- **Path**: Database file path
- **Options**: Feature flags (FTS, JSON, Vector)
- **Extensions**: Extensions to load (e.g., sqlite-vec for vector search)

```go
type StoreConfig struct {
    Type       string
    Path       string
    Options    collection.Options
    Extensions []ExtensionConfig
}
```

### Extension Loading

SQLite extensions (like `sqlite-vec` for vector search) can be loaded via the factory. Extensions require a CGo connection, which is automatically opened when extensions are provided.

```go
store, err := db.NewStore(ctx, db.StoreConfig{
    Path: "./data.db",
    Options: collection.Options{
        EnableVector: true,  // Capability flag (must be true for vector support)
    },
    Extensions: []db.ExtensionConfig{
        {
            Path:       "/path/to/sqlite-vec.so",
            EntryPoint: "sqlite3_vec_init",
            Required:   true,  // Fail if extension can't load
        },
    },
})
```

**Extension Behavior:**
- **Required extensions** (`Required: true`): Store creation fails if extension can't load
- **Optional extensions** (`Required: false`): Store is created, but the extension capability is unavailable
- **CGo requirement**: Extensions require a CGo connection (`vectorDB`), which is automatically opened when extensions are provided
- **EnableVector flag**: This is a capability flag only - it does NOT trigger CGo connection opening. Both `EnableVector: true` AND extensions are required for vector support

### Capability Detection

Stores support runtime capability detection via the `Supports()` method:

```go
if store.Supports(db.CapabilityVector) {
    // Vector search is available
}

if store.Supports(db.CapabilityFTS) {
    // Full-text search is available
}

if store.Supports(db.CapabilityJSON) {
    // JSONB operators are available
}
```

**Capability Constants:**
- `db.CapabilityVector` - Vector search support (requires CGo + extensions + EnableVector)
- `db.CapabilityFTS` - Full-text search support (requires EnableFTS)
- `db.CapabilityJSON` - JSONB operator support (requires EnableJSON)

## Hybrid Model: Pure Go + CGo

The SQLite implementation uses a **hybrid dual-connection architecture**:

- **Pure Go connection** (`modernc.org/sqlite`): 
  - Always opened for all regular operations (CRUD, FTS, JSON)
  - No CGo required - works on all platforms
  - Fast compilation and deployment
  
- **CGo connection** (`sqliteext`): 
  - Only opened when extensions are provided
  - Used exclusively for vector search operations (when implemented)
  - Extensions loaded only on this connection

**Benefits:**
- Most operations use pure Go (fast, portable)
- CGo only when needed (lightweight, optional)
- Works without CGo for 99% of use cases
- Better than fully CGo-based drivers

**Connection Behavior:**
- Both connections point to the same database file
- SQLite WAL mode handles concurrent access safely
- Regular operations never touch the CGo connection
- Vector operations (when implemented) will use the CGo connection exclusively

## Error Handling

### ExtensionError

Returned when a required extension fails to load:

```go
store, err := db.NewStore(ctx, db.StoreConfig{
    Extensions: []db.ExtensionConfig{
        {Path: "/nonexistent.so", Required: true},
    },
})

if db.IsExtensionError(err) {
    extErr := err.(*db.ExtensionError)
    fmt.Printf("Failed to load extension: %s\n", extErr.Extension)
}
```

### CapabilityError

Returned when attempting to use an unsupported capability:

```go
if !store.Supports(db.CapabilityVector) {
    return &db.CapabilityError{
        Feature: db.CapabilityVector,
        Message: "Vector search not available: CGo driver or extensions not loaded",
    }
}
```

## Implementation Details

### SQLite Package Structure

```
pkg/db/sqlite/
├── store.go          # Main SQLite store implementation
├── ext/              # CGo driver for extensions (internal)
│   ├── driver.go     # CGo implementation (build tag: cgo)
│   └── driver_stub.go # Stub for non-CGo builds (build tag: !cgo)
└── backup_test.go    # Backup-specific tests
```

### Extension Loading Flow

1. Factory receives `StoreConfig` with extensions
2. Factory calls `sqlite.NewSqliteStore()`
3. **Pure Go connection** is always opened first (for regular operations)
4. If extensions are provided:
   - **CGo connection** is opened (for vector operations only)
   - Extensions loaded via `ext.Conn.LoadExtension()` on CGo connection
5. Store returned with both connections (vectorDB is nil if no extensions provided)

**Note:** `EnableVector` is a capability flag only - it does NOT trigger CGo connection opening. Extensions must be explicitly provided to open the CGo connection.

### Capability Detection Flow

1. Store tracks enabled options and connection availability
2. `Supports()` checks:
   - **Vector**: Requires `EnableVector` AND `vectorDB != nil` (CGo connection exists)
   - **FTS**: Requires `EnableFTS` option (uses pure Go connection)
   - **JSON**: Requires `EnableJSON` option (uses pure Go connection)

## Examples

### Basic Store

```go
store, err := db.NewStore(ctx, db.StoreConfig{
    Path: "./data.db",
})
```

### Store with FTS and JSON

```go
store, err := db.NewStore(ctx, db.StoreConfig{
    Path: "./data.db",
    Options: collection.Options{
        EnableFTS:  true,
        EnableJSON: true,
    },
})
```

### Store with Vector Search (Extension Setup)

```go
store, err := db.NewStore(ctx, db.StoreConfig{
    Path: "./data.db",
    Options: collection.Options{
        EnableVector: true,  // Capability flag
    },
    Extensions: []db.ExtensionConfig{
        {
            Path:       "/usr/lib/sqlite-vec.so",
            EntryPoint: "sqlite3_vec_init",
            Required:   true,
        },
    },
})

if store.Supports(db.CapabilityVector) {
    // Vector search available (requires both EnableVector AND extensions)
    // Note: Vector search implementation is not yet complete
}
```

**Important:** `EnableVector` is a capability flag only. The CGo connection is opened **only when extensions are provided**. Both `EnableVector: true` and extensions are required for `Supports(CapabilityVector)` to return `true`.

### Memory Database

```go
store, err := db.NewStore(ctx, db.StoreConfig{
    Path: ":memory:",
    Options: collection.Options{
        EnableJSON: true,
    },
})
```

## Adding New Database Types

To add a new database type (e.g., PostgreSQL):

1. **Implement the `Store` interface** in a new package (e.g., `pkg/db/postgres/`)

   The `Store` interface is defined in `pkg/collection/repo.go`. You'll need to implement:
   - `CreateRecord`, `GetRecord`, `UpdateRecord`, `DeleteRecord`, `ListRecords`
   - `Search`, `CountRecords`
   - `ExecuteRaw`, `Supports`, `Close`, `Path`
   - `Backup`, `BackupOnline`, `Checkpoint`, `ReIndex`

2. **Add factory case** in `pkg/db/store.go`:

```go
func NewStore(ctx context.Context, config StoreConfig) (collection.Store, error) {
    storeType := config.Type
    if storeType == "" {
        storeType = "sqlite"
    }

    switch storeType {
    case "sqlite":
        return newSqliteStore(ctx, config)
    case "postgres":  // NEW
        return newPostgresStore(ctx, config)
    default:
        return nil, fmt.Errorf("unsupported store type: %s (supported: sqlite)", storeType)
    }
}
```

3. **Implement `newPostgresStore()`** following the SQLite pattern:
   - Parse `StoreConfig` to extract database-specific settings
   - Create database connection
   - Apply schemas and migrations
   - Return a `Store` implementation

4. **Update tests** in `pkg/db/store_test.go` to cover the new database type

5. **Update this README** to document the new database type

## Adding Vector Search

Vector search is not yet implemented, but the infrastructure is in place. To implement it:

1. **Extension Loading** (already implemented):
   - Load `sqlite-vec` extension via `Extensions` in `StoreConfig`
   - Extension loads on the CGo connection (`vectorDB`)

2. **Vector Search Implementation** (to be implemented):
   - Add vector search methods to the `Store` interface (or extend `Search` method)
   - Implement vector search in `pkg/db/sqlite/store.go` using the `vectorDB` connection
   - Use SQLite vector functions (e.g., `vec_search`, `vec_distance`) from the loaded extension

3. **Capability Detection** (already implemented):
   - `Supports(db.CapabilityVector)` returns `true` when:
     - `EnableVector: true` is set
     - Extensions are provided (CGo connection exists)
     - Extension loads successfully

4. **Example Usage** (when implemented):
```go
// Vector search will use the CGo connection
results, err := store.VectorSearch(ctx, queryVector, limit)
```

## Testing

The package includes comprehensive tests in `store_test.go`:

- Factory creation with various configurations
- Capability detection
- Extension loading (with graceful CGo handling)
- Error handling
- Store operations

Run tests:
```bash
go test ./pkg/db -v
```

**Note**: CGo-dependent tests may fail on macOS. Tests are designed to run on Linux systems with CGo enabled.

## See Also

- [Collection Package](../collection/README.md) - Uses the Store interface
- [SQLite Implementation](./sqlite/store.go) - SQLite-specific implementation
- [Store Interface](../collection/repo.go) - Store interface definition
