# Dispatch Package

The dispatch package implements the CollectiveDispatcher service, enabling distributed RPC routing across a cluster of Collector instances. It provides transparent service discovery, request routing, and execution orchestration across multiple collectors.

## Overview

CollectiveDispatcher acts as a service mesh router that:
- Connects collectors into a distributed network
- Routes RPC requests to the appropriate collector
- Executes service methods locally when available
- Validates requests against the registry
- Maintains connection topology and namespace mappings

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   Collector A                       │
│                                                     │
│  ┌──────────────────────────────────────────────┐ │
│  │           Dispatcher                         │ │
│  │                                              │ │
│  │  • Connection Manager                        │ │
│  │  • Service Registry                          │ │
│  │  • Request Router                            │ │
│  │  • Registry Validator                        │ │
│  └─────────────┬────────────────────────────────┘ │
│                │                                    │
└────────────────┼────────────────────────────────────┘
                 │
                 │ gRPC connections
                 │
    ┌────────────┼────────────┐
    │            │            │
    ▼            ▼            ▼
┌─────────┐  ┌─────────┐  ┌─────────┐
│Collector│  │Collector│  │Collector│
│    B    │  │    C    │  │    D    │
└─────────┘  └─────────┘  └─────────┘
```

## Core RPCs

### 1. Connect - Establish Collector Links

Creates a bidirectional connection between two collectors.

**Request:**
```protobuf
message ConnectRequest {
  string collector_id = 1;        // ID of collector initiating connection
  string address = 2;             // gRPC address of initiating collector
  repeated string namespaces = 3; // Namespaces supported by initiator
}
```

**Response:**
```protobuf
message ConnectResponse {
  Status status = 1;
  string connection_id = 2;              // Unique connection identifier
  repeated string shared_namespaces = 3; // Intersection of both collectors' namespaces
  string target_collector_id = 4;        // ID of target collector
}
```

**Example:**
```go
// Collector A connects to Collector B
resp, err := dispatcherA.ConnectTo(ctx, "collector-b:50051", []string{"users", "orders"})
// If Collector B supports ["orders", "products"], shared_namespaces = ["orders"]
```

**Behavior:**
- Computes shared namespaces (intersection of both collectors' namespaces)
- Creates gRPC client connection to target collector
- Stores connection metadata with shared namespace info
- Connection is bidirectional - both collectors can route to each other

### 2. Serve - Execute Local Service Methods

Executes a service method on the local collector.

**Request:**
```protobuf
message ServeRequest {
  string namespace = 1;
  ServiceTypeRef service = 2;
  string method_name = 3;
  google.protobuf.Any input = 4;
}
```

**Response:**
```protobuf
message ServeResponse {
  Status status = 1;
  google.protobuf.Any output = 2;
  string executor_id = 3;  // ID of collector that executed the request
}
```

**Example:**
```go
// Register a service handler
dispatcher.RegisterService("users", "UserService", "GetUser",
    func(ctx context.Context, input interface{}) (interface{}, error) {
        userID := input.(string)
        return getUserFromDB(userID)
    })

// Execute via Serve
resp, err := client.Serve(ctx, &pb.ServeRequest{
    Namespace:  "users",
    Service:    &pb.ServiceTypeRef{ServiceName: "UserService"},
    MethodName: "GetUser",
    Input:      anyPb,
})
```

**Validation Flow:**
```
Serve Request
    │
    ├─> Registry Validator (if configured)
    │   ├─> ValidateServiceMethod(namespace, service, method)
    │   └─> Returns error if not registered
    │
    ├─> Lookup Service Handler
    │   └─> Returns error if not found locally
    │
    └─> Execute Handler
        └─> Return response with executor_id
```

### 3. Dispatch - Smart Request Routing

High-level API that combines connection topology with service execution. Routes requests to the appropriate collector automatically.

**Request:**
```protobuf
message DispatchRequest {
  string namespace = 1;
  ServiceTypeRef service = 2;
  string method_name = 3;
  google.protobuf.Any input = 4;
  string target_collector_id = 5;  // Optional: specific target
}
```

**Response:**
```protobuf
message DispatchResponse {
  Status status = 1;
  google.protobuf.Any output = 2;
  string handled_by_collector_id = 3;  // Which collector executed it
}
```

**Routing Modes:**

#### Target-Specific Routing
Route to a specific collector by ID:
```go
resp, err := client.Dispatch(ctx, &pb.DispatchRequest{
    Namespace:         "orders",
    Service:           &pb.ServiceTypeRef{ServiceName: "OrderService"},
    MethodName:        "CreateOrder",
    Input:             orderData,
    TargetCollectorId: "collector-west-2",
})
```

#### Auto-Routing
Let the dispatcher find the right collector:
```go
// No target specified - dispatcher auto-routes
resp, err := client.Dispatch(ctx, &pb.DispatchRequest{
    Namespace:  "orders",
    Service:    &pb.ServiceTypeRef{ServiceName: "OrderService"},
    MethodName: "CreateOrder",
    Input:      orderData,
})
// Returns handled_by_collector_id indicating which collector executed it
```

**Auto-Routing Logic:**
1. **Check local**: Is the method registered locally? Execute and return
2. **Find remote**: Look for connected collector with shared namespace
3. **Forward**: Call remote collector's Serve RPC
4. **Return**: Proxy response back to client with handled_by_collector_id

## Service Registration

Register handlers for local service execution:

```go
// Create dispatcher
dispatcher := dispatch.NewDispatcher(
    "collector-001",
    "localhost:50051",
    []string{"users", "orders"},
)

// Register a service handler
dispatcher.RegisterService(
    "users",                // namespace
    "UserService",          // service name
    "GetUser",              // method name
    func(ctx context.Context, input interface{}) (interface{}, error) {
        // Your implementation
        return result, nil
    },
)
```

Multiple services per namespace are supported:
```go
dispatcher.RegisterService("users", "UserService", "GetUser", getUserHandler)
dispatcher.RegisterService("users", "UserService", "CreateUser", createUserHandler)
dispatcher.RegisterService("users", "AuthService", "Login", loginHandler)
```

## Registry Integration

The dispatcher integrates with the Registry service for validation:

```go
// Create validator from registry
validator := registry.NewGRPCRegistryValidator(grpcValidator)

// Create dispatcher with validation
dispatcher := dispatch.NewDispatcherWithRegistry(
    "collector-001",
    "localhost:50051",
    []string{"production"},
    validator,
)
```

**Validation happens before execution:**
```go
// In Serve():
if d.registryValidator != nil {
    err := d.registryValidator.ValidateServiceMethod(
        ctx, req.Namespace, req.Service.ServiceName, req.MethodName,
    )
    if err != nil {
        // Return 404 - service not registered
        return &pb.ServeResponse{
            Status: &pb.Status{Code: 404, Message: "service not registered"},
        }, nil
    }
}
```

**Backwards Compatible:**
- If no validator is configured, validation is skipped
- Old code without registry continues to work
- Validation can be added dynamically via `SetRegistryValidator()`

## Connection Management

### Establishing Connections

```go
// Connect to another collector
resp, err := dispatcher1.ConnectTo(
    ctx,
    "localhost:50052",           // Target address
    []string{"users", "orders"}, // Your namespaces
)

// Connection is bidirectional
// Both collectors can now route to each other
```

### Connection Topology

Connections support mesh topologies:
```
     A ──┬── B
     │   │
     │   └── C
     │
     └── D
```

Each collector maintains:
- Connection map: `collector_id → gRPC client`
- Namespace map: `namespace → []collector_id`

### Shared Namespaces

Only namespaces supported by BOTH collectors are active:
```go
// Collector A: ["users", "orders", "products"]
// Collector B: ["orders", "products", "inventory"]
// Shared: ["orders", "products"]

// Requests in "users" or "inventory" won't route between them
// Requests in "orders" or "products" will
```

## Complete Example

```go
package main

import (
    "context"
    "net"

    "github.com/accretional/collector/pkg/dispatch"
    pb "github.com/accretional/collector/gen/collector"
    "google.golang.org/grpc"
)

func main() {
    ctx := context.Background()

    // ============================================================
    // Setup Collector 1
    // ============================================================

    dispatcher1 := dispatch.NewDispatcher(
        "collector-1",
        "localhost:50051",
        []string{"users", "orders"},
    )

    // Register service handler on Collector 1
    dispatcher1.RegisterService("users", "UserService", "GetUser",
        func(ctx context.Context, input interface{}) (interface{}, error) {
            return "User from collector-1", nil
        })

    // Start gRPC server for Collector 1
    lis1, _ := net.Listen("tcp", "localhost:50051")
    server1 := grpc.NewServer()
    pb.RegisterCollectiveDispatcherServer(server1, dispatcher1)
    go server1.Serve(lis1)

    // ============================================================
    // Setup Collector 2
    // ============================================================

    dispatcher2 := dispatch.NewDispatcher(
        "collector-2",
        "localhost:50052",
        []string{"orders", "inventory"},
    )

    // Register service handler on Collector 2
    dispatcher2.RegisterService("orders", "OrderService", "CreateOrder",
        func(ctx context.Context, input interface{}) (interface{}, error) {
            return "Order created on collector-2", nil
        })

    // Start gRPC server for Collector 2
    lis2, _ := net.Listen("tcp", "localhost:50052")
    server2 := grpc.NewServer()
    pb.RegisterCollectiveDispatcherServer(server2, dispatcher2)
    go server2.Serve(lis2)

    // ============================================================
    // Connect Collectors
    // ============================================================

    // Collector 1 connects to Collector 2
    dispatcher1.ConnectTo(ctx, "localhost:50052", []string{"users", "orders"})
    // Shared namespace: ["orders"]

    // ============================================================
    // Client Makes Request
    // ============================================================

    conn, _ := grpc.Dial("localhost:50051", grpc.WithInsecure())
    client := pb.NewCollectiveDispatcherClient(conn)

    // Request to "users" namespace - executes on Collector 1 (local)
    resp1, _ := client.Dispatch(ctx, &pb.DispatchRequest{
        Namespace:  "users",
        Service:    &pb.ServiceTypeRef{ServiceName: "UserService"},
        MethodName: "GetUser",
        Input:      ...,
    })
    // resp1.HandledByCollectorId == "collector-1"

    // Request to "orders" namespace - routes to Collector 2 (remote)
    resp2, _ := client.Dispatch(ctx, &pb.DispatchRequest{
        Namespace:  "orders",
        Service:    &pb.ServiceTypeRef{ServiceName: "OrderService"},
        MethodName: "CreateOrder",
        Input:      ...,
    })
    // resp2.HandledByCollectorId == "collector-2"
}
```

## Request Flow Diagram

```
┌──────────┐
│  Client  │
└────┬─────┘
     │
     │ Dispatch(namespace: "orders", service: "OrderService", method: "CreateOrder")
     │
     ▼
┌─────────────────────────────────────┐
│       Collector 1 Dispatcher        │
│                                     │
│  1. Check if "OrderService" local  │
│     → Not found locally            │
│                                     │
│  2. Find connection with "orders"  │
│     → Found: Collector 2           │
│                                     │
│  3. Forward via Serve RPC          │
└──────────────┬──────────────────────┘
               │
               │ Serve(namespace: "orders", ...)
               │
               ▼
┌─────────────────────────────────────┐
│       Collector 2 Dispatcher        │
│                                     │
│  4. Validate (if configured)       │
│     → Check registry               │
│                                     │
│  5. Lookup service handler         │
│     → Found: OrderService handler  │
│                                     │
│  6. Execute handler                │
│     → result = createOrder(...)    │
│                                     │
│  7. Return ServeResponse           │
│     executor_id: "collector-2"     │
└──────────────┬──────────────────────┘
               │
               │ ServeResponse
               │
               ▼
┌─────────────────────────────────────┐
│       Collector 1 Dispatcher        │
│                                     │
│  8. Receive response from Coll 2   │
│                                     │
│  9. Return DispatchResponse        │
│     handled_by: "collector-2"      │
└──────────────┬──────────────────────┘
               │
               │ DispatchResponse
               │
               ▼
         ┌──────────┐
         │  Client  │
         └──────────┘
```

## Testing

The package includes comprehensive tests:

```bash
go test ./pkg/dispatch/... -v
```

**Test Coverage:**
- Connection tests (basic, bidirectional, multiple, shared namespaces, real network)
- Serve tests (invocation, error handling, invalid requests, multiple services)
- Dispatch tests (target-specific, local routing, remote routing, error cases)
- Registry validation tests (valid/invalid services, namespace isolation)

## Key Interfaces

### RegistryValidator Interface

```go
type RegistryValidator interface {
    ValidateServiceMethod(ctx context.Context, namespace, serviceName, methodName string) error
}
```

Implement this interface to provide custom validation logic or integrate with the Registry service.

### ServiceHandler Function

```go
type ServiceHandler func(ctx context.Context, input interface{}) (interface{}, error)
```

All service handlers must match this signature. Input and output can be any type - typically proto messages.

## Best Practices

1. **Use namespaces for isolation**: Different environments, tenants, or feature sets
2. **Register services early**: Before accepting requests
3. **Enable validation**: Connect to Registry for type safety
4. **Handle errors gracefully**: Check connection status, validate responses
5. **Monitor routing**: Log handled_by_collector_id for observability
6. **Test connection topology**: Verify shared namespaces are correct

## Performance Considerations

- **Local execution is fast**: No network hop when handler is local
- **Connection pooling**: gRPC reuses connections efficiently
- **Namespace filtering**: Only route to collectors with shared namespaces
- **Concurrent requests**: Dispatcher is thread-safe, handles concurrent calls

## Future Enhancements

- Load balancing across multiple collectors with same namespace
- Circuit breakers for failing collectors
- Request tracing and distributed logging
- Connection health checks and auto-reconnection
- Dynamic namespace updates
- Service mesh integration (Istio, Linkerd)
