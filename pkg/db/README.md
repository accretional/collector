# Database Package

The `db` package provides a factory pattern for creating database stores with support for multiple database backends, extension loading, and capability detection.

## Overview

The database package abstracts database creation behind a unified factory interface (`db.NewStore()`), allowing the system to support multiple database types (currently SQLite, with PostgreSQL planned) while hiding implementation details from higher layers.

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
│  SQLite Store   │  │  PostgreSQL      │
│                 │  │  (Future)        │
│  • Pure Go      │  │                  │
│  • CGo + Exts   │  │                  │
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

- **Type**: Database type (`"sqlite"` is default, PostgreSQL planned)
- **Path**: Database file path (or connection string for future DBs)
- **Options**: Feature flags (FTS, JSON, Vector)
- **Extensions**: SQLite extensions to load (e.g., sqlite-vec)

```go
type StoreConfig struct {
    Type       string              // "sqlite" (default)
    Path       string              // Database path
    Options    collection.Options  // Feature flags
    Extensions []ExtensionConfig   // Extensions to load
}
```

### Extension Loading

SQLite extensions (like `sqlite-vec` for vector search) can be loaded via the factory:

```go
store, err := db.NewStore(ctx, db.StoreConfig{
    Path: "./data.db",
    Options: collection.Options{
        EnableVector: true,  // Enables CGo driver
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
- **Required extensions**: Store creation fails if extension can't load
- **Optional extensions**: Store is created, but capability is unavailable
- **CGo requirement**: Extensions require the CGo driver (`sqliteext`), which is automatically selected when `EnableVector` is true or extensions are specified

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
- `db.CapabilityVector` - Vector search support (requires CGo + extensions)
- `db.CapabilityFTS` - Full-text search support
- `db.CapabilityJSON` - JSONB operator support

### Driver Selection

The SQLite implementation automatically selects the appropriate driver:

- **Pure Go driver** (`modernc.org/sqlite`): Used by default, no CGo required
- **CGo driver** (`sqliteext`): Used when:
  - `EnableVector` option is true, OR
  - Extensions are specified

This selection is transparent to the caller - the factory handles it automatically.

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

Returned when attempting to use an unsupported capability (future use):

```go
if !store.Supports(db.CapabilityVector) {
    return &db.CapabilityError{
        Feature: db.CapabilityVector,
        Message: "CGo driver not available",
    }
}
```

## Adding New Database Types

To add a new database type (e.g., PostgreSQL):

1. **Implement the `Store` interface** in a new package (e.g., `pkg/db/postgres/`)

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
        return nil, fmt.Errorf("unsupported store type: %s", storeType)
    }
}
```

3. **Implement `newPostgresStore()`** following the SQLite pattern

4. **Update tests** to cover the new database type

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
3. SQLite package detects extensions → selects CGo driver
4. Database opened with `sqliteext` driver
5. Extensions loaded via `ext.Conn.LoadExtension()`
6. Store returned with capabilities set

### Capability Detection Flow

1. Store tracks enabled options and driver type
2. `Supports()` checks:
   - **Vector**: Requires `EnableVector` AND `driver == "sqliteext"`
   - **FTS**: Requires `EnableFTS` option
   - **JSON**: Requires `EnableJSON` option

## Migration from Direct SQLite Calls

**Before (direct SQLite):**
```go
import "github.com/accretional/collector/pkg/db/sqlite"

store, err := sqlite.NewSqliteStore(ctx, path, opts, nil)
```

**After (factory pattern):**
```go
import "github.com/accretional/collector/pkg/db"

store, err := db.NewStore(ctx, db.StoreConfig{
    Path:    path,
    Options: opts,
})
```

**Benefits:**
- ✅ Database-agnostic code (easy to switch backends)
- ✅ Consistent error handling
- ✅ Extension loading support
- ✅ Capability detection
- ✅ Future-proof for new database types

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

### Store with Vector Search

```go
store, err := db.NewStore(ctx, db.StoreConfig{
    Path: "./data.db",
    Options: collection.Options{
        EnableVector: true,
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
    // Vector search available
}
```

### Memory Database

```go
store, err := db.NewStore(ctx, db.StoreConfig{
    Path: ":memory:",
    Options: collection.Options{
        EnableJSON: true,
    },
})
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

## Future Work

- [ ] PostgreSQL backend implementation
- [ ] Connection pooling configuration
- [ ] Transaction management abstraction
- [ ] Migration system
- [ ] Query builder abstraction

## See Also

- [Collection Package](../collection/README.md) - Uses the Store interface
- [SQLite Implementation](./sqlite/store.go) - SQLite-specific implementation
- [Store Interface](../collection/repo.go) - Store interface definition

