# Collector Documentation

Complete documentation for the Collector distributed system framework.

## Quick Links

- **[Main README](../README.md)** - Project overview and quick start
- **[Features Documentation](#features)** - Detailed feature guides
- **[Architecture Documentation](#architecture)** - System design and components
- **[Testing Documentation](#testing)** - Test results and verification

## Features

### Data Management

- **[Backup API](features/backup-api.md)** - Point-in-time snapshots and disaster recovery
  - BackupCollection, RestoreBackup, ListBackups, DeleteBackup, VerifyBackup
  - Retention management and integrity verification
  - Near-zero downtime backups (proven with tests)

- **[Clone and Fetch](features/clone-and-fetch.md)** - Collection replication and migration
  - Local and remote collection cloning
  - Streaming data transfer for large collections
  - Database and filesystem cloning

### Core Services

- **[Registry Service](../pkg/registry/README.md)** - Service and type registration
  - Type-safe service validation
  - Dynamic service discovery
  - Namespace-based isolation

- **[Collection Service](../pkg/collection/README.md)** - ORM for protobuf messages
  - CRUD operations with full-text search
  - JSONB filtering and complex queries
  - File attachments and hierarchical storage

- **[Dispatcher](../pkg/dispatch/README.md)** - Distributed RPC routing
  - Transparent multi-collector routing
  - Namespace-aware dispatch
  - Registry-validated execution

- **[Collection Repository](../pkg/collection/README.md#collectionrepo)** - Multi-collection management
  - Dynamic collection creation
  - Cross-collection search
  - Collection discovery

## Architecture

### Core Concepts

- **Namespace-Based Isolation** - Multi-tenancy and environment separation
- **Type-Safe Dynamic Dispatch** - Protobuf typing with runtime flexibility
- **gRPC All the Way** - Uniform communication even for co-located services
- **Collection-Oriented Storage** - Everything stored in collections

### Components

```
┌─────────────────────────────────────────┐
│         Collector Instance              │
│                                         │
│  ┌───────────────────────────────────┐ │
│  │    Single gRPC Server             │ │
│  │                                   │ │
│  │  ├─ CollectorRegistry            │ │
│  │  ├─ CollectionService            │ │
│  │  ├─ CollectiveDispatcher         │ │
│  │  └─ CollectionRepo                │ │
│  │        ├─ Backup (NEW)            │ │
│  │        ├─ Clone/Fetch (NEW)       │ │
│  │        └─ Search                  │ │
│  └───────────────────────────────────┘ │
└─────────────────────────────────────────┘
```

### Data Flow

**Backup Operations:**
```
Client → CollectionRepo.BackupCollection
       → BackupManager.BackupCollection
       → SQLite VACUUM INTO (6-14ms lock)
       → Backup metadata stored
       → Response with backup ID
```

**Clone Operations:**
```
Local Clone:
  Client → CollectionRepo.Clone
         → CloneManager.CloneLocal
         → Transport.Clone (database backup)
         → Create new collection metadata

Remote Clone:
  Client → CollectionRepo.Clone (with dest_endpoint)
         → CloneManager.CloneRemote
         → Open gRPC streaming (PushCollection)
         → Stream 1MB chunks to remote
         → Remote creates collection
```

## Testing

### Test Coverage

- **[Backup Availability Tests](testing/backup-availability.md)** - Proven near-zero downtime
  - 402-641 concurrent reads during backup (0 errors)
  - 24-40 concurrent writes during backup (0 errors)
  - 6-14ms lock duration (well below thresholds)
  - Production load simulation passing

### Running Tests

```bash
# All tests
go test ./pkg/... -v

# Backup tests
go test ./pkg/collection -run "Test.*Backup" -v

# SQLite backup/availability tests
go test ./pkg/db/sqlite -run TestBackup -v

# Integration tests
go test ./pkg/integration -v
```

### Test Statistics

- **215+ tests** across all packages
- **100% passing** (with 1 skipped file test)
- **14 backup-specific tests** (all passing)
- **7 backup availability tests** (all passing)
- **Integration tests** validate multi-collector scenarios

## Performance

### Benchmarks

| Operation | Performance |
|-----------|-------------|
| CRUD operations | 1-2ms |
| Full-text search (100k records) | 10-50ms |
| Backup (100 records) | 3-5ms |
| Backup (10k records) | 50-100ms |
| Backup lock duration | 6-14ms |
| Loopback gRPC overhead | 100μs-1ms |
| Remote gRPC | 10-100ms |

### Concurrent Operations During Backup

| Metric | Value |
|--------|-------|
| Concurrent reads | 400+ ops/sec |
| Concurrent writes | 25+ ops/sec |
| Read errors | 0 |
| Write errors | 0 |
| Availability | 100% |

## Use Cases

### 1. Point-in-Time Backup
```go
// Create daily backup
resp, _ := client.BackupCollection(ctx, &pb.BackupCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    DestPath:   "/backups/users-2025-11-22.db",
    IncludeFiles: true,
    Metadata:   map[string]string{"retention": "30d"},
})
```

### 2. Collection Migration
```go
// Clone to staging for testing
resp, _ := client.Clone(ctx, &pb.CloneRequest{
    SourceCollection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    DestNamespace: "staging",
    DestName:      "users-test",
})
```

### 3. Disaster Recovery
```go
// List recent backups
listResp, _ := client.ListBackups(ctx, &pb.ListBackupsRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    Limit: 10,
})

// Restore from backup
restoreResp, _ := client.RestoreBackup(ctx, &pb.RestoreBackupRequest{
    BackupId:      listResp.Backups[0].BackupId,
    DestNamespace: "prod",
    DestName:      "users",
    Overwrite:     true,
})
```

### 4. Multi-Collector Replication
```go
// Fetch collection from remote collector
resp, _ := client.Fetch(ctx, &pb.FetchRequest{
    SourceEndpoint: "collector1:50051",
    SourceCollection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    DestNamespace: "prod",
    DestName:      "users-mirror",
})
```

## Development

### Building

```bash
# Build server
go build ./cmd/server

# Run server
go run ./cmd/server/main.go

# Generate protobuf code
./scripts/gen-proto.sh
```

### Project Structure

```
collector/
├── cmd/server/              # Main executable
├── pkg/
│   ├── registry/            # Service registry
│   ├── collection/          # ORM and data storage
│   │   ├── backup.go        # Backup manager (NEW)
│   │   ├── clone.go         # Clone/fetch operations (NEW)
│   │   ├── transport.go     # Data transport layer (NEW)
│   │   └── fetch.go         # Remote fetching (NEW)
│   ├── dispatch/            # Distributed routing
│   ├── db/sqlite/           # SQLite backend
│   │   ├── store.go
│   │   └── backup_test.go   # Availability tests (NEW)
│   └── fs/local/            # Filesystem abstraction (NEW)
├── proto/                   # Protocol buffers
│   ├── collection_repo.proto  # Backup/Clone RPCs (UPDATED)
│   └── ...
├── docs/                    # Documentation
│   ├── features/            # Feature guides
│   ├── architecture/        # System design
│   └── testing/             # Test results
└── data/                    # Runtime data
    ├── backups/             # Backup storage (NEW)
    │   └── metadata.db      # Backup metadata (NEW)
    └── collections/         # Collection databases
```

## Contributing

See the main [README](../README.md) for contribution guidelines.

## License

See the main [README](../README.md) for license information.
