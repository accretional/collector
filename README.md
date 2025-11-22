# Collector

A gRPC + Protocol Buffers framework for building distributed, dynamic RPC systems with built-in service discovery, type safety, and a powerful ORM for protobuf messages.

## What is Collector?

Collector is a distributed programming platform that combines:
- **Service Registry**: Type-safe registration and validation of gRPC services
- **Collections**: ORM-like storage for protobuf messages with full-text search
- **Dynamic Dispatch**: Transparent distributed RPC routing across clusters
- **Reflection & Discovery**: Runtime service introspection and dynamic invocation

It enables you to register and update protobuf messages and gRPC services at runtime, create "Collections" (tables + API servers) of any proto type, and dynamically dispatch RPC calls across a distributed system—all with strong typing and validation.

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
- JSONB filtering for complex queries
- File attachments (hierarchical file storage)
- Custom RPC handlers
- Batch operations

**Key RPCs:**
- `Create` / `Get` / `Update` / `Delete` / `List` - CRUD
- `Search` - Full-text + JSONB queries
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

**Key RPCs:**
- `CreateCollection` - Create new collection
- `Discover` - Find collections
- `Route` - Get collection endpoint
- `SearchCollections` - Cross-collection search

**Documentation**: [pkg/collection/README.md](pkg/collection/README.md#collectionrepo---multi-collection-management)

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
    conn, _ := grpc.Dial("localhost:50051", grpc.WithInsecure())
    defer conn.Close()

    ctx := context.Background()

    // 1. Create a collection
    repoClient := pb.NewCollectionRepoClient(conn)
    createResp, _ := repoClient.CreateCollection(ctx, &pb.CreateCollectionRequest{
        Collection: &pb.Collection{
            Namespace:   "production",
            Name:        "users",
            MessageType: "collector.User",
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
        CollectorId: "collector-001",
        Address:     "localhost:50051",
        Namespaces:  []string{"production"},
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

// Combined with JSONB filtering
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

## Data Model

### Collections

Collections are like database tables for protobuf messages:

```
Collection: production/users
  ├─ Store: SQLite with JSONB + FTS5
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

Comprehensive test coverage across all packages:

```bash
# Run all tests
go test ./pkg/... -v

# Run specific package tests
go test ./pkg/registry/... -v
go test ./pkg/dispatch/... -v
go test ./pkg/collection/... -v

# Run integration tests
go test ./pkg/integration/... -v
```

**Test Statistics:**
- 215+ tests total
- All packages: 100% passing
- Integration tests validate multi-collector scenarios
- End-to-end tests prove full system integration

## Building

```bash
# Build the server
go build ./cmd/server

# Build and run
go run ./cmd/server/main.go

# Generate protobuf code (if proto files change)
./scripts/gen-proto.sh
```

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
│   │   └── README.md
│   │
│   ├── dispatch/        # Distributed routing
│   │   ├── dispatcher.go
│   │   ├── connection.go
│   │   └── README.md
│   │
│   ├── db/
│   │   └── sqlite/      # SQLite backend
│   │
│   └── integration/     # Integration tests
│       ├── e2e_test.go
│       └── multi_collector_test.go
│
├── proto/               # Protocol buffer definitions
│   ├── common.proto
│   ├── collection.proto
│   ├── dispatch.proto
│   └── registry.proto
│
├── gen/                 # Generated protobuf code
│   └── collector/
│
└── data/                # Runtime data (created at startup)
    ├── registry/        # Registry collections
    ├── repo/            # Collection repository
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
- [ ] CollectorConsole UI/analysis tools
- [ ] GraphQL interface for collections
- [ ] Cross-collector registry replication
- [ ] Query optimizer for complex searches
- [ ] Schema evolution and migrations
- [ ] Streaming APIs for large result sets

## Contributing

This is an experimental framework exploring new patterns in distributed systems. Feedback, issues, and contributions welcome!

## License

[Add your license here]

## For LLMs, Agents, and Developers

Collector is built for all three:
- **LLMs**: Use natural language to describe data models, get type-safe storage
- **Agents**: Discover capabilities via registry, invoke operations dynamically
- **Developers**: Build distributed systems with strong typing and minimal boilerplate

The framework bridges human intent, AI capabilities, and production systems through a unified protobuf-based interface.

---

**Ready to build?** Start with `go run ./cmd/server/main.go` and explore the package READMEs for deep dives into each service.
