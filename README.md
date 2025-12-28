# Collector

A gRPC + Protocol Buffers framework for building distributed, dynamic RPC systems with built-in service discovery, type safety, and a powerful ORM for protobuf messages.

### HUMAN NOTE
Contact [fred](https://www.linkedin.com/in/fred-weitendorf-40b505b6/) if you want to try using this! Smash that star button to get updates as we try to get it production ready, ~January 2026.

## What is Collector?

Collector is a distributed programming platform that combines:
- **Service Registry**: Type-safe registration and validation of gRPC services
- **Collections**: ORM-like storage for protobuf messages with full-text search
- **Dynamic Dispatch**: Transparent distributed RPC routing across clusters
- **Reflection & Discovery**: Runtime service introspection and dynamic invocation

It enables you to register and update protobuf messages and gRPC services at runtime, create "Collections" (tables + API servers) of any proto type, and dynamically dispatch RPC calls across a distributed system—all with strong typing and validation.

<details><summary>Tell me more!</summary>

Collections are generic protobuf container for structured data, backed by sqlite. A collection is an ordered list of records of a single proto message type, filterable labels, and create/update metadata.

Namespaces are the core of collector's multitenancy model. A collection belongs to a single namespace, but the services respect a hierarchical namespacing model that keeps data/types/services/everything else separate across namespaces.

CollectionService is implements generic CRUD and Search APIs for Collections, plus the ability to invoke custom rpcs on the Collections. These are all provided automatically.

CollectionRepo is a Collection of Collections, "controller" of collection service and other internal/registered grpc servers and collections for this "collector". Also handles backups, clones/fetches, etc.

CollectiveDispatcher implements Connect, Dispatch, and Serve rpcs across Collectors. This is what turns Collector into a node in a mesh. It also allows execution to move to data or find available compute dynamically.

CollectorRegistry provides a registry of proto messages and grpcs with some reflection functionality. The rpc/message registry are what make this work as an "agent mesh" - it allows agents to interact with remote collectors, or remote collectors to interact with the local agent, in more compact/api-based interfaces than just text.Z

</details>

## Using it

CollectionService alone is kind of like a protobuf ORM/CRUD server with search. CollectionRepo also gets you backups and db operations, including backups. 

Paired with [statue](https://github.com/accretional/statue) these sqlite-based "collections" can be used as a portable/snapshotted generic container for structured data, that you can distribute and query on a static site!

Enable CollectiveDispatcher and CollectorRegistry to get something with all the right characteristics of a node in an "agent mesh" - hierarchical multitenancy, data/service/type discovery, dynamic tool creation, interoperability with humans or traditional stateful instances/stateless services (which can also run collector!). Note: you should probably run this in a secure sandbox.

**TL;DR: Collector is meant to serve as a node in an agent mesh, providing everything a tool-calling LLM needs to discovery/share/find/manage data and tools for itself and in conjunction with other agents. But it's also a library or service you can use yourself to implement a generic CRUD server for protobufs and grpc with powerful, dynamic distributed programming/reflection.**

Most of the rest of the docs were written by robots, but reviewed by humans with love in San Francisco.

## Architecture

### Single Collector

Each collector runs **one gRPC server** with **all services** registered:

```
┌─────────────────────────────────────────┐
│         Collector Instance              │
│                                         │
│  ┌───────────────────────────────────┐ │
│  │    Single gRPC Server             │ │
│  │    (port 50051)                   │ │
│  │                                   │ │
│  │  ├─ CollectorRegistry            │ │
│  │  ├─ CollectionService            │ │
│  │  ├─ CollectiveDispatcher         │ │
│  │  └─ CollectionRepo                │ │
│  │                                   │ │
│  │  Registry Validation: ENABLED    │ │
│  └───────────────────────────────────┘ │
└─────────────────────────────────────────┘
```

### Multi-Collector Cluster

Multiple collectors connect to form a distributed system:

```
┌──────────────────┐         ┌──────────────────┐
│  Collector 1     │◄───────►│  Collector 2     │
│  localhost:50051 │         │  localhost:50052 │
│                  │         │                  │
│  All 4 services  │         │  All 4 services  │
│  With validation │         │  With validation │
└──────────────────┘         └──────────────────┘
        ▲                            ▲
        │                            │
        └──────────┬─────────────────┘
                   │
              Dispatcher
              connects and
              routes between
```

### Service-to-Service Communication

Services communicate via **gRPC loopback** even when co-located:

```
┌─────────────────────────────────────────────────┐
│          Single gRPC Server (port 50051)        │
│                                                 │
│  ┌──────────────┐         ┌──────────────┐    │
│  │  Dispatcher  │ ─────>  │   Registry   │    │
│  │              │  gRPC   │              │    │
│  └──────────────┘  call   └──────────────┘    │
│         │              via loopback             │
│         └──────────────────┐                   │
│                            ▼                   │
│                    localhost:50051             │
└─────────────────────────────────────────────────┘
                            │
                            │ (actual gRPC call)
                            ▼
                    gRPC validation interceptor
                            │
                            ▼
                    Registry.ValidateMethod()
```

**Why loopback?**
- ✅ Validates server wiring (ensures all services properly registered)
- ✅ Consistent behavior (same code path as remote calls)
- ✅ Full gRPC features (interceptors, middleware, error handling)
- ✅ Type safety (registry validation applies)

## Core Services

### 1. CollectorRegistry

**Purpose**: Centralized service and type registry

**Capabilities:**
- Register protobuf message types and gRPC services
- Validate RPC calls against registered types
- Dynamic service discovery and lookup
- Namespace-based isolation

**Key RPCs:**
- `RegisterProto` / `RegisterService` - Register types
- `LookupService` / `ValidateMethod` - Query registry
- `ListServices` - Discover available services

**Documentation**: [pkg/registry/README.md](pkg/registry/README.md)

### 2. CollectionService

**Purpose**: ORM-like storage for protobuf messages

**Capabilities:**
- CRUD operations (Create, Get, Update, Delete, List)
- Full-text search (SQLite FTS5)
- JSON filtering for complex queries
- File attachments (hierarchical file storage)
- Custom RPC handlers
- Batch operations

**Key RPCs:**
- `Create` / `Get` / `Update` / `Delete` / `List` - CRUD
- `Search` - Full-text + JSON queries
- `Invoke` - Custom method execution
- `Batch` - Multi-operation transactions

**Documentation**: [pkg/collection/README.md](pkg/collection/README.md)

### 3. CollectiveDispatcher

**Purpose**: Distributed RPC routing

**Capabilities:**
- Connect collectors into a mesh network
- Route requests to appropriate collector
- Execute service methods locally or remotely
- Namespace-aware routing
- Registry-validated execution

**Key RPCs:**
- `Connect` - Establish collector-to-collector links
- `Serve` - Execute local service methods
- `Dispatch` - Smart request routing (local or remote)

**Documentation**: [pkg/dispatch/README.md](pkg/dispatch/README.md)

### 4. CollectionRepo

**Purpose**: Multi-collection management

**Capabilities:**
- Create collections dynamically
- Discover collections by namespace, message type, or labels
- Route requests to appropriate collection
- Search across multiple collections
- **🆕 Backup and restore collections** (point-in-time snapshots)
- **🆕 Clone collections** (local and remote replication)
- **🆕 Fetch collections** (pull from remote collectors)

**Key RPCs:**
- `CreateCollection` - Create new collection
- **🆕 `DeleteCollection`** - Delete collection and all data
- `Discover` - Find collections
- `Route` - Get collection endpoint
- `SearchCollections` - Cross-collection search
- **🆕 `BackupCollection`** - Create point-in-time backup
- **🆕 `RestoreBackup`** - Restore from backup
- **🆕 `ListBackups` / `DeleteBackup` / `VerifyBackup`** - Backup management
- **🆕 `Clone`** - Clone collection (local or remote)
- **🆕 `Fetch`** - Pull collection from remote collector

**Documentation**:
- [pkg/collection/README.md](pkg/collection/README.md#collectionrepo---multi-collection-management)
- **🆕 [Backup API Guide](docs/features/backup-api.md)** - Complete backup documentation
- **🆕 [Clone & Fetch Guide](docs/features/clone-and-fetch.md)** - Replication and migration

## Using Collector as a Library

Collector can be easily embedded in your own Go applications. Instead of copying boilerplate code from `main.go`, use the `pkg/server` package:

```go
package main

import (
    "log"
    "github.com/accretional/collector/pkg/server"
)

func main() {
    // Create and start the Collector server
    srv, err := server.New(server.Config{
        DataDir:   "./my-app-data",
        Port:      8080,
        Namespace: "my-app",
        // CollectorID auto-generates a UUID7 if not specified
    })
    if err != nil {
        log.Fatalf("Failed to create server: %v", err)
    }
    defer srv.Close()

    // Server is now running with all services available:
    // - CollectorRegistry
    // - CollectionService
    // - CollectiveDispatcher
    // - CollectionRepo

    // Your application logic here...
    // Connect to srv.Address() to use the services

    // Wait for shutdown signal (Ctrl+C)
    srv.WaitForShutdown()
}
```

**Configuration Options:**
- `DataDir` - Root directory for all data storage (default: `"./data"`)
- `Port` - gRPC server port (default: `50051`)
- `Namespace` - Default namespace for this collector (default: `"shared"`)
- `CollectorID` - Unique identifier for this collector (default: random UUID7)
- `Logger` - Custom logger (default: `log.Default()`)

**Reserved Namespaces:**
- `repo`, `backups`, `files` - Reserved (conflict with filesystem paths)
- `system` - Used for internal collections (types, collections, connections, audit, logs) but not blocked
- Use your own namespace for application data (e.g., `"my-app"`, `"shared"`, `"production"`)

**See also:** [examples/embedded/main.go](examples/embedded/main.go) for a complete example.

## Quick Start

### Running a Collector

```bash
# Run the server
go run ./cmd/server/main.go
```

Output:
```
Starting Collector (ID: collector-001, Namespace: production)
✓ Registry server created
✓ Registered CollectionService in namespace 'production'
✓ Registered CollectiveDispatcher in namespace 'production'
✓ Registered CollectionRepo in namespace 'production'
✓ Collection repository created
✓ Dispatcher created with gRPC-based registry validation

========================================
Collector collector-001 running on localhost:50051
All services available:
  - CollectorRegistry
  - CollectionService
  - CollectiveDispatcher
  - CollectionRepo
Namespace: production
Registry validation: ENABLED
========================================
Press Ctrl+C to shutdown
```

### Client Example

```go
package main

import (
    "context"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    pb "github.com/accretional/collector/gen/collector"
)

func main() {
    // Connect to collector
    conn, _ := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
    defer conn.Close()

    ctx := context.Background()

    // 1. Create a collection
    repoClient := pb.NewCollectionRepoClient(conn)
    createResp, _ := repoClient.CreateCollection(ctx, &pb.CreateCollectionRequest{
        Collection: &pb.Collection{
            Namespace: "production",
            Name:      "users",
            MessageType: &pb.MessageTypeRef{
                Namespace:   "myapp",
                MessageName: "User",
            },
        },
    })

    // 2. Insert a record
    collectionClient := pb.NewCollectionServiceClient(conn)
    user := &pb.User{Id: "user-123", Name: "Alice", Email: "alice@example.com"}
    createResp, _ := collectionClient.Create(ctx, &pb.CreateRequest{
        Collection: &pb.Collection{Namespace: "production", Name: "users"},
        Record:     &pb.Record{Id: "user-123", Data: marshalToAny(user)},
    })

    // 3. Search records
    searchResp, _ := collectionClient.Search(ctx, &pb.SearchRequest{
        Collection: &pb.Collection{Namespace: "production", Name: "users"},
        Query:      "alice",
        Limit:      10,
    })

    // 4. Connect to another collector
    dispatcherClient := pb.NewCollectiveDispatcherClient(conn)
    connectResp, _ := dispatcherClient.Connect(ctx, &pb.ConnectRequest{
        Address:    "localhost:50052",
        Namespaces: []string{"production"},
        Metadata: map[string]string{
            "collector_id": "collector-001",
        },
    })

    // 5. Dispatch a request (routes automatically)
    dispatchResp, _ := dispatcherClient.Dispatch(ctx, &pb.DispatchRequest{
        Namespace:  "production",
        Service:    &pb.ServiceTypeRef{ServiceName: "CollectionService"},
        MethodName: "Get",
        Input:      getRequestAny,
    })
}
```

## Key Features

### Namespace-Based Isolation

Everything in Collector is namespaced:
- **Multi-tenancy**: Different tenants have isolated data and services
- **Environment separation**: Dev/staging/prod with different configurations
- **Feature flags**: Enable/disable services per namespace
- **Version management**: Run multiple versions simultaneously

```go
// Register service in production namespace
registry.RegisterCollectionService(ctx, registryServer, "production")

// Register different version in staging
registry.RegisterCollectionServiceV2(ctx, registryServer, "staging")
```

### Type-Safe RPC Validation

All RPCs are validated against the registry before execution:

```go
// Create server with automatic validation
grpcServer := registry.NewServerWithValidation(registryServer, "production")

// Register service
pb.RegisterCollectionServiceServer(grpcServer, collectionServer)

// Unregistered RPCs are automatically rejected with codes.Unimplemented
```

### Dynamic Service Discovery

Query available services at runtime:

```go
// List all services in a namespace
services, _ := registryClient.ListServices(ctx, &pb.ListServicesRequest{
    Namespace: "production",
})

for _, service := range services {
    fmt.Printf("Service: %s\n", service.ServiceName)
    fmt.Printf("Methods: %v\n", service.MethodNames)
}
```

### Full-Text Search

SQLite FTS5-powered search across protobuf messages:

```go
// Search with full-text query
results, _ := client.Search(ctx, &pb.SearchRequest{
    Collection: &pb.Collection{Namespace: "production", Name: "users"},
    Query:      "senior engineer",
    Limit:      20,
})

// Combined with JSON filtering
results, _ := client.Search(ctx, &pb.SearchRequest{
    Collection: &pb.Collection{Namespace: "production", Name: "users"},
    Query:      "engineer",
    Filters: []*pb.SearchFilter{
        {Field: "status", Operator: pb.SearchOperator_EQUALS, Value: "active"},
        {Field: "years_exp", Operator: pb.SearchOperator_GREATER_THAN, Value: "5"},
    },
    OrderBy: "created_at",
    Desc:    true,
})
```

### Distributed Routing

Transparent RPC routing across collectors:

```go
// Client calls Collector A
resp, _ := client.Dispatch(ctx, &pb.DispatchRequest{
    Namespace:  "orders",
    Service:    &pb.ServiceTypeRef{ServiceName: "OrderService"},
    MethodName: "CreateOrder",
    Input:      orderData,
    // No target specified - auto-routes to appropriate collector
})

// resp.HandledByCollectorId tells you which collector executed it
fmt.Printf("Executed by: %s\n", resp.HandledByCollectorId)
```

### Backup and Replication 🆕

**Point-in-time backups** with automatic retention management:

```go
// Step 1: Create collection with retention policy (one-time setup)
_, _ = repoClient.CreateCollection(ctx, &pb.CreateCollectionRequest{
    Collection: &pb.Collection{
        Namespace:   "prod",
        Name:        "users",
        MessageType: &pb.MessageTypeRef{MessageName: "User"},
        BackupPolicy: &pb.BackupPolicy{
            MaxBackups:       7,              // Keep last 7 backups
            RetentionSeconds: 30 * 24 * 3600, // 30 days
            Enabled:          true,           // Enable automatic cleanup
        },
    },
})

// Step 2: Create backup (path is auto-generated)
backupResp, _ := client.BackupCollection(ctx, &pb.BackupCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    IncludeFiles: true,
    Metadata:     map[string]string{"type": "daily"},
})
// Old backups are automatically deleted based on retention policy!

// List backups
listResp, _ := client.ListBackups(ctx, &pb.ListBackupsRequest{
    Collection: &pb.NamespacedName{Namespace: "prod", Name: "users"},
    Limit:      10,
})

// Restore from backup
restoreResp, _ := client.RestoreBackup(ctx, &pb.RestoreBackupRequest{
    BackupId:      backupResp.Backup.BackupId,
    DestNamespace: "prod",
    DestName:      "users",
})
```

**Collection cloning** for testing and migration:

```go
// Local clone (within same collector)
cloneResp, _ := client.Clone(ctx, &pb.CloneRequest{
    SourceCollection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    DestNamespace: "staging",
    DestName:      "users-test",
    IncludeFiles:  true,
})

// Remote clone (to another collector)
cloneResp, _ := client.Clone(ctx, &pb.CloneRequest{
    SourceCollection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    DestEndpoint:  "collector2:50051",  // Remote target
    DestNamespace: "prod",
    DestName:      "users",
})

// Fetch from remote (pull collection)
fetchResp, _ := client.Fetch(ctx, &pb.FetchRequest{
    SourceEndpoint: "collector1:50051",
    SourceCollection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    DestNamespace: "prod",
    DestName:      "users-mirror",
})
```

**Key capabilities:**
- ✅ **Near-zero downtime** during backup (6-14ms lock time, proven with tests)
- ✅ **Concurrent operations** during backup (400+ reads/sec, 25+ writes/sec)
- ✅ **Streaming transfers** for large collections (1MB chunks)
- ✅ **Integrity verification** (SQLite PRAGMA checks)
- ✅ **Retention management** (list, delete old backups)

See: [Backup API Documentation](docs/features/backup-api.md) | [Clone & Fetch Guide](docs/features/clone-and-fetch.md)

## Data Model

### Collections

Collections are like database tables for protobuf messages:

```
Collection: production/users
  ├─ Store: SQLite with JSON + FTS5
  │   ├─ user-123: {name: "Alice", email: "alice@example.com"}
  │   ├─ user-456: {name: "Bob", email: "bob@example.com"}
  │   └─ ...
  │
  └─ FileSystem: Hierarchical file storage
      ├─ user-123/
      │   ├─ profile.jpg
      │   └─ documents/
      │       ├─ resume.pdf
      │       └─ cover-letter.pdf
      └─ user-456/
          └─ avatar.png
```

### Registry Storage

Registry stores type information in collections:

```
RegisteredProtos Collection (system namespace)
  └─ production/User → FileDescriptorProto + metadata

RegisteredServices Collection (system namespace)
  └─ production/CollectionService → ServiceDescriptorProto + methods
```

## Design Philosophy

### Everything is Namespaced

Namespaces provide the fundamental isolation boundary:
- Data is scoped to namespaces
- Services are registered per namespace
- Validation is namespace-specific
- Routing respects namespace boundaries

### Strong Typing with Dynamic Dispatch

- All messages are typed (protobuf)
- All services are registered (type-checked)
- But invocation is dynamic (runtime dispatch)
- Best of both worlds: safety + flexibility

### gRPC All the Way Down

- Service-to-service communication via gRPC (even same-server)
- Interceptors apply uniformly
- Same code path for local and remote
- Proper observability and middleware

### Collection-Oriented Storage

- Registry stores service definitions in collections
- Collections store user data
- Collections can contain collections
- Uniform interface for all data

## Testing

**⚠️ IMPORTANT: Testing Requirements**

**Before making ANY changes, run the comprehensive test suite:**

```bash
./RUN_ALL_TESTS_BEFORE_SUBMIT.sh
```

This is **mandatory** for:
- ✅ All code changes
- ✅ All pull requests
- ✅ All AI agent contributions
- ✅ All manual development

**For AI Agents:** See [AGENTS.md](AGENTS.md) for detailed guidelines. Any test failure is YOUR responsibility to fix.

### Comprehensive Test Suite (REQUIRED)

**Always run the full test suite before submitting changes:**

```bash
./RUN_ALL_TESTS_BEFORE_SUBMIT.sh
```

This script runs:
- ✅ Build verification
- ✅ Code quality checks (go vet, go fmt)
- ✅ All unit tests
- ✅ Integration tests
- ✅ Backup system validation
- ✅ Concurrency & race detection
- ✅ Durability tests
- ✅ Benchmarks
- ✅ Coverage report

### Running Individual Test Suites

For development and debugging:

```bash
# Run all tests
go test ./pkg/... -v

# Run specific package tests
go test ./pkg/registry/... -v
go test ./pkg/dispatch/... -v
go test ./pkg/collection/... -v

# Run backup tests
go test ./pkg/collection -run "Test.*Backup" -v

# Run SQLite backup/availability tests
go test ./pkg/db/sqlite -run TestBackup -v

# Run integration tests
go test ./pkg/integration/... -v

# Race detection
go test ./pkg/... -race -short
```

**Test Statistics:**
- **230+ tests total** (215 existing + 15 new backup/availability tests)
- All packages: **100% passing**
- **14 backup-specific tests** - CRUD, concurrency, large datasets, special characters
- **7 backup availability tests** - Proven near-zero downtime with concurrent operations
- Integration tests validate multi-collector scenarios
- End-to-end tests prove full system integration

**Backup Availability Proof** (measured results):
- ✅ **402-641 concurrent reads** during backup with 0 errors
- ✅ **24-40 concurrent writes** during backup with 0 errors
- ✅ **6-14ms lock duration** (well below 50-200ms thresholds)
- ✅ **Production load test**: 340 reads + 25 writes simultaneously, all successful

See: [Backup Availability Test Results](docs/testing/backup-availability.md)

## Building

```bash
# Build the server
go build -tags sqlite_fts5 ./cmd/server

# Build and run
go run -tags sqlite_fts5 ./cmd/server/main.go

# Run tests
go test -tags sqlite_fts5 ./...

# Generate protobuf code (if proto files change)
./scripts/gen-proto.sh
```

### Dependencies

- **[mattn/go-sqlite3](https://github.com/mattn/go-sqlite3)**: CGO-based SQLite driver. Requires a C compiler (gcc/clang) for building.
- **[asg017/sqlite-vec](https://github.com/asg017/sqlite-vec)**: Vector search extension for SQLite (optional, for semantic search).

### Build Tags

- **`sqlite_fts5`**: Enables FTS5 (Full-Text Search) support in SQLite. This is **required** for full-text search functionality and must be included in all build and test commands.

## Project Structure

```
collector/
├── cmd/
│   └── server/          # Main server executable
│       └── main.go
│
├── pkg/
│   ├── registry/        # Service registry and validation
│   │   ├── registry.go
│   │   ├── interceptor.go
│   │   ├── helpers.go
│   │   └── README.md
│   │
│   ├── collection/      # ORM and data storage
│   │   ├── collection.go
│   │   ├── collection_server.go
│   │   ├── repo.go
│   │   ├── grpc_server.go
│   │   ├── backup.go            # 🆕 Backup manager
│   │   ├── backup_test.go       # 🆕 Backup tests (14 tests)
│   │   ├── clone.go             # 🆕 Clone/fetch operations
│   │   ├── transport.go         # 🆕 Data transport layer
│   │   ├── fetch.go             # 🆕 Remote fetching
│   │   └── README.md
│   │
│   ├── dispatch/        # Distributed routing
│   │   ├── dispatcher.go
│   │   ├── connection.go
│   │   └── README.md
│   │
│   ├── db/              # Database factory
│   │   ├── store.go     # Factory for creating stores
│   │   ├── README.md    # How to add new backends
│   │   └── sqlite/      # SQLite backend
│   │       ├── store.go
│   │       ├── store_test.go
│   │       └── backup_test.go   # Availability tests
│   │
│   ├── fs/              # 🆕 Filesystem abstraction
│   │   └── local/       # 🆕 Local filesystem implementation
│   │
│   └── integration/     # Integration tests
│       ├── e2e_test.go
│       └── multi_collector_test.go
│
├── proto/               # Protocol buffer definitions
│   ├── common.proto
│   ├── collection.proto
│   ├── collection_repo.proto    # 🆕 Backup/Clone RPCs added
│   ├── dispatch.proto
│   └── registry.proto
│
├── gen/                 # Generated protobuf code
│   └── collector/
│
├── docs/                # 🆕 Organized documentation
│   ├── README.md        # 🆕 Documentation index
│   ├── features/        # 🆕 Feature guides
│   │   ├── backup-api.md          # 🆕 Backup API documentation
│   │   └── clone-and-fetch.md     # 🆕 Clone/Fetch guide
│   ├── architecture/    # 🆕 System design docs
│   └── testing/         # 🆕 Test results
│       └── backup-availability.md # 🆕 Availability proof
│
└── data/                # Runtime data (created at startup)
    ├── registry/        # Registry collections
    ├── repo/            # Collection repository
    ├── backups/         # 🆕 Backup storage
    │   └── metadata.db  # 🆕 Backup metadata tracking
    └── files/           # File attachments
```

## Use Cases

### 1. Multi-Tenant SaaS

```go
// Each tenant gets their own namespace
for _, tenant := range tenants {
    // Register services per tenant
    registry.RegisterCollectionService(ctx, registryServer, tenant.ID)

    // Create tenant-specific collections
    collectionRepo.CreateCollection(ctx, &pb.CreateCollectionRequest{
        Collection: &pb.Collection{
            Namespace:   tenant.ID,
            Name:        "users",
            MessageType: "app.User",
        },
    })
}
```

### 2. Dynamic API Server

```go
// Register a new message type at runtime
registryClient.RegisterProto(ctx, &pb.RegisterProtoRequest{
    Namespace:      "production",
    FileDescriptor: newProtoDescriptor,
})

// Create a collection for it
collectionRepo.CreateCollection(ctx, &pb.CreateCollectionRequest{
    Collection: &pb.Collection{
        Namespace:   "production",
        Name:        "new-entity",
        MessageType: "app.NewEntity",
    },
})

// CRUD API is immediately available!
```

### 3. Distributed Microservices

```go
// Collector 1: User service
dispatcher1.RegisterService("users", "UserService", "GetUser", getUserHandler)

// Collector 2: Order service
dispatcher2.RegisterService("orders", "OrderService", "CreateOrder", createOrderHandler)

// Connect collectors
dispatcher1.ConnectTo(ctx, "collector2:50052", []string{"users", "orders"})

// Client calls Collector 1, transparently routes to Collector 2 when needed
```

### 4. Agent/LLM Backend

Dynamic dispatch and reflection make Collector ideal for agent systems:
- Register new capabilities as protobuf messages
- Agents discover available operations via registry
- Type-safe invocation with runtime flexibility
- Search across structured agent memory (collections)

## Security Considerations

**⚠️ Important**: Allowing clients to register types and invoke arbitrary methods is powerful but dangerous. Use in controlled environments or with additional security layers:

1. **Sandboxed Execution**: Run `Serve` methods in containers
2. **Authentication**: Add auth interceptors to gRPC servers
3. **Authorization**: Validate namespace access per user/tenant
4. **Rate Limiting**: Limit registration and RPC frequency
5. **Input Validation**: Validate all inputs in service handlers

The Dispatcher's `Serve` method is designed as an extension point for adding security controls.

## Performance

### Benchmarks

- **CRUD operations**: ~1-2ms per operation
- **Full-text search**: ~10-50ms for 100k records
- **Loopback gRPC**: ~100μs-1ms overhead
- **Remote gRPC**: ~10-100ms depending on network

### Scaling

- **Vertical**: SQLite WAL mode enables high concurrency
- **Horizontal**: Add collectors, connect via Dispatcher
- **Sharding**: Use namespaces to partition data
- **Caching**: Registry lookups can be cached

## Roadmap

### Near Term
- [ ] Add dedicated `ValidateMethod` RPC to Registry
- [ ] Implement caching for registry validation
- [ ] Health checks via loopback connections
- [ ] Metrics and distributed tracing

### Future
- [ ] CollectiveWorker workflow system
- [ ] Cross-collector registry replication
- [ ] Query optimizer for complex searches
- [ ] Schema evolution and migrations
- [ ] Streaming APIs for large result sets

## Contributing

This is an experimental framework exploring new patterns in distributed systems. Feedback, issues, and contributions welcome!

## License

MIT

## For LLMs, Agents, and Developers

Collector is built for all three:
- **LLMs**: Use natural language to describe data models, get type-safe storage
- **Agents**: Discover capabilities via registry, invoke operations dynamically
- **Developers**: Build distributed systems with strong typing and minimal boilerplate

The framework bridges human intent, AI capabilities, and production systems through a unified protobuf-based interface.

---

**Ready to build?** Start with `go run ./cmd/server/main.go` and explore the package READMEs for deep dives into each service.
