# Vector Search Implementation

This document explains the architecture behind vector search in the collector's SQLite store, including the technology choices and the hybrid driver approach.

## Technology Choices

### Why modernc.org/sqlite?

We use `modernc.org/sqlite` as our primary SQLite driver for several reasons:

| Feature | modernc.org/sqlite | mattn/go-sqlite3 |
|---------|-------------------|------------------|
| CGo requirement | No (pure Go) | Yes |
| Cross-compilation | Simple | Requires C toolchain for each target |2
| Build speed | Fast | Slower (C compilation) |
| Binary size | Slightly larger | Smaller |
| Deployment | Single binary, no dependencies | May need SQLite shared library |
| Windows/macOS/Linux | Works out of the box | Needs platform-specific setup |

The pure Go implementation makes builds reproducible and simplifies CI/CD pipelines. Developers don't need a C compiler installed, and cross-compiling for different platforms (e.g., building Linux binaries on macOS) works without additional tooling.

### Why sqlite-vec?

For vector similarity search, we chose [sqlite-vec](https://github.com/asg017/sqlite-vec) over alternatives:

| Solution | Pros | Cons |
|----------|------|------|
| **sqlite-vec** | Native SQLite extension, lightweight (~150KB), fast KNN search, no external dependencies | Requires extension loading |
| sqlite-vss | More features (IVF indexes) | Larger, more complex, less maintained |
| Pure Go (e.g., hnswgo) | No CGo needed | Separate index management, not integrated with SQL |

sqlite-vec provides:
- **vec0 virtual table**: Stores vectors alongside regular data
- **KNN queries**: `WHERE vector MATCH ? AND k = ?` syntax
- **Multiple distance metrics**: L2 (Euclidean), cosine, inner product
- **Small footprint**: Single `.so` file, ~150KB

## The Integration Challenge

### The Problem

sqlite-vec is a **native C extension** that must be loaded at runtime via SQLite's extension loading API:

```c
sqlite3_enable_load_extension(db, 1);
sqlite3_load_extension(db, "vec0.so", "sqlite3_vec_init", &errmsg);
```

However, `modernc.org/sqlite` is a **pure Go transpilation** of SQLite. It doesn't support loading native C extensions because:

1. The extension loading code calls into C's dynamic linker (`dlopen`/`LoadLibrary`)
2. Extensions are compiled C code that expects to link against SQLite's C symbols
3. The pure Go version doesn't expose these C-level interfaces

### Options Considered

1. **Switch entirely to mattn/go-sqlite3**: Would work, but loses all benefits of pure Go builds
2. **Port sqlite-vec to Go**: Massive effort, performance concerns
3. **Use a separate vector database**: Adds operational complexity
4. **Hybrid driver approach**: Use CGo only for vector operations

## The Solution: Hybrid Driver Architecture

We implemented a **minimal CGo-based driver** specifically for vector operations, while keeping `modernc.org/sqlite` for everything else.

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    SQLite Database File                      │
│                      (WAL mode)                              │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌───────────────────────┐    ┌───────────────────────┐    │
│  │   modernc.org/sqlite  │    │   Custom CGo Driver   │    │
│  │      (pure Go)        │    │    (sqlite3_vec)      │    │
│  │                       │    │                       │    │
│  │  • CRUD operations    │    │  • Load sqlite-vec    │    │
│  │  • FTS5 searches      │    │  • vec0 table ops     │    │
│  │  • JSON queries       │    │  • KNN searches       │    │
│  │  • Schema management  │    │                       │    │
│  │                       │    │                       │    │
│  │  s.db                 │    │  s.vecDB              │    │
│  └───────────────────────┘    └───────────────────────┘    │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Implementation Details

#### Custom CGo Driver (`cgo_vec.go`)

A minimal SQLite driver that:
- Links against the system SQLite C library
- Implements `database/sql/driver` interfaces
- Exposes `LoadExtension()` for loading sqlite-vec
- Handles only what's needed for vector operations

```go
//go:build cgo && !novector

/*
#cgo LDFLAGS: -lsqlite3
#include <sqlite3.h>
*/
import "C"

type VecDriver struct{}
type VecConn struct{ db *C.sqlite3 }

func (c *VecConn) LoadExtension(path, entry string) error {
    // Calls C.sqlite3_load_extension()
}
```

#### Stub for Non-CGo Builds (`cgo_vec_stub.go`)

When CGo is disabled or `-tags=novector` is used:

```go
//go:build !cgo || novector

func OpenVecDB(...) (*VecDB, error) {
    return nil, fmt.Errorf("vector search requires CGo build")
}
```

#### Store Integration (`store.go`)

The `SqliteStore` maintains two connections:

```go
type SqliteStore struct {
    db       *sql.DB   // modernc.org/sqlite - regular operations
    vecDB    *VecDB    // CGo driver - vector operations
    // ...
}
```

Operations are routed appropriately:
- `CreateRecord`, `UpdateRecord`, `DeleteRecord`: Use `db` for record data, `vecDB` for vec0 table
- `Search` with vector: Uses `vecDB` for KNN query
- `Search` without vector: Uses `db` for FTS/JSON queries

### Concurrency Considerations

Both connections access the same database file. SQLite's WAL mode allows:
- Multiple concurrent readers
- One writer at a time (with busy timeout)

The store uses mutex locks to coordinate operations that span both connections:

```go
s.mu.Lock()
defer s.mu.Unlock()

// 1. Insert record via db (modernc)
tx.ExecContext(ctx, "INSERT INTO records ...")
tx.Commit()

// 2. Insert vector via vecDB (CGo) - must happen after commit
//    so vecDB can see the new rowid
s.vecDB.ExecContext(ctx, "INSERT INTO records_vec ...")
```

## Usage

### Build Requirements

For vector search support:
```bash
# Install SQLite development headers
apt-get install libsqlite3-dev  # Debian/Ubuntu
brew install sqlite3            # macOS

# Build with CGo (default)
go build ./...
```

For pure Go builds (no vector search):
```bash
CGO_ENABLED=0 go build ./...
# or
go build -tags=novector ./...
```

### Runtime Requirements

Set the extension path via environment variable:
```bash
export SQLITE_VEC_EXTENSION=/path/to/vec0.so
```

Or place the extension at the default location: `./sqlite-vec/vec0.so`

### Downloading sqlite-vec

```bash
# Linux x86_64
curl -L https://github.com/asg017/sqlite-vec/releases/download/v0.1.6/sqlite-vec-0.1.6-loadable-linux-x86_64.tar.gz | tar xz

# macOS arm64
curl -L https://github.com/asg017/sqlite-vec/releases/download/v0.1.6/sqlite-vec-0.1.6-loadable-macos-aarch64.tar.gz | tar xz
```

## Query Syntax

sqlite-vec uses a special KNN query syntax:

```sql
-- Basic KNN search
SELECT rowid, distance
FROM records_vec
WHERE vector MATCH '[1.0, 2.0, 3.0, ...]'
  AND k = 10
ORDER BY distance;

-- Join with records table
SELECT r.id, r.proto_data, v.distance
FROM records_vec v
JOIN records r ON r.rowid = v.rowid
WHERE v.vector MATCH ? AND k = ?
ORDER BY v.distance;
```

Note: The `k = ?` constraint is **required** for KNN queries in sqlite-vec.

## Trade-offs

| Aspect | Impact |
|--------|--------|
| Build complexity | CGo required only when vector search is enabled |
| Cross-compilation | Needs C toolchain for target platform (for vector builds) |
| Two connections | Slightly more memory, coordination overhead |
| Atomicity | Vector index updates happen after main record commit |
| Portability | Extension must be compiled for each target OS/arch |

## Future Considerations

1. **Bundled extension**: Could embed sqlite-vec source and compile it with the driver
2. **Pure Go vector search**: If a suitable Go library emerges
3. **Alternative backends**: Could abstract vector search behind an interface for different implementations
