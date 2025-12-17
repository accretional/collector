# Building Collector

Collector supports two build modes: a pure Go build for standard database operations, and a CGo build that adds vector search capabilities.

## Quick Start

### Without Vector Search (Pure Go)

No C compiler required. All standard features work: CRUD, FTS5 full-text search, JSON filters, labels, and backups.

```bash
CGO_ENABLED=0 go build ./...
```

### With Vector Search (CGo)

Requires a C compiler and libsqlite3 development headers.

```bash
# Install dependencies (Ubuntu/Debian)
sudo apt-get install build-essential libsqlite3-dev

# Install dependencies (macOS)
brew install sqlite3

# Build
CGO_ENABLED=1 go build ./...
```

## Vector Search Setup

Vector search requires the `sqlite-vec` extension. Follow these steps to enable it:

### 1. Download the sqlite-vec Extension

Download the pre-built extension for your platform from the [sqlite-vec releases](https://github.com/asg017/sqlite-vec/releases):

```bash
# Create extension directory
mkdir -p sqlite-vec

# Linux (x86_64)
curl -L https://github.com/asg017/sqlite-vec/releases/download/v0.1.6/sqlite-vec-0.1.6-loadable-linux-x86_64.tar.gz | tar -xz -C sqlite-vec

# Linux (ARM64)
curl -L https://github.com/asg017/sqlite-vec/releases/download/v0.1.6/sqlite-vec-0.1.6-loadable-linux-aarch64.tar.gz | tar -xz -C sqlite-vec

# macOS (Apple Silicon)
curl -L https://github.com/asg017/sqlite-vec/releases/download/v0.1.6/sqlite-vec-0.1.6-loadable-macos-aarch64.tar.gz | tar -xz -C sqlite-vec

# macOS (Intel)
curl -L https://github.com/asg017/sqlite-vec/releases/download/v0.1.6/sqlite-vec-0.1.6-loadable-macos-x86_64.tar.gz | tar -xz -C sqlite-vec
```

The extension file will be at `sqlite-vec/vec0.so` (Linux) or `sqlite-vec/vec0.dylib` (macOS).

### 2. Configure Extension Path (Optional)

The extension is auto-discovered by searching for `sqlite-vec/vec0.so` starting from the current directory and walking up to the root. This works automatically if you place the extension in your project root.

Alternatively, set the environment variable explicitly using an **absolute path**:

```bash
# Linux
export SQLITE_VEC_EXTENSION=$(pwd)/sqlite-vec/vec0.so

# macOS
export SQLITE_VEC_EXTENSION=$(pwd)/sqlite-vec/vec0.dylib
```

### 3. Build with CGo

```bash
CGO_ENABLED=1 go build ./...
```

### 4. Enable Vector Search in Code

```go
store, err := sqlite.NewSqliteStore(dbPath, collection.Options{
    EnableVector:     true,
    VectorDimensions: 384,  // Must match your embedding model
    EnableFTS:        true,
    EnableJSON:       true,
}, embedder)
```

## Feature Comparison

| Feature | Pure Go Build | CGo Build |
|---------|---------------|-----------|
| CRUD operations | Yes | Yes |
| FTS5 full-text search | Yes | Yes |
| JSON filters | Yes | Yes |
| Label filters | Yes | Yes |
| Backup/Restore | Yes | Yes |
| **Vector search** | No | Yes |

## Troubleshooting

### "extension loading requires CGo build"

You're trying to use vector search with a pure Go build. Rebuild with `CGO_ENABLED=1`.

### "failed to load extension"

- Verify the extension file exists at the configured path
- Check file permissions (`chmod +x vec0.so`)
- Ensure the extension matches your platform architecture

### "libsqlite3.so: cannot open shared object file"

Install SQLite development libraries:

```bash
# Ubuntu/Debian
sudo apt-get install libsqlite3-dev

# Fedora/RHEL
sudo dnf install sqlite-devel

# macOS
brew install sqlite3
```

### CGo build fails on macOS

You may need to specify the SQLite path:

```bash
export CGO_CFLAGS="-I/opt/homebrew/opt/sqlite/include"
export CGO_LDFLAGS="-L/opt/homebrew/opt/sqlite/lib"
CGO_ENABLED=1 go build ./...
```

## Running Tests

```bash
# Run all tests (pure Go)
CGO_ENABLED=0 go test ./...

# Run all tests including CGo driver tests
CGO_ENABLED=1 go test ./...

# Run only sqliteext driver tests
CGO_ENABLED=1 go test ./pkg/db/sqliteext/... -v
```
