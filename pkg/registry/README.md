# Registry Package

The registry package provides centralized service and type registration for the Collector system. It enables type-safe RPC validation, dynamic service discovery, and proper service-to-service communication through gRPC.

## Overview

The Registry service acts as a type system and service catalog that:
- Registers protobuf message types and gRPC services
- Validates RPC calls against registered types
- Provides dynamic service discovery and lookup
- Enforces namespace-based isolation
- Enables proper gRPC-based service-to-service communication

## Architecture

```
┌──────────────────────────────────────────────────┐
│              Client Request                       │
└───────────────────┬──────────────────────────────┘
                    │
                    ▼
┌──────────────────────────────────────────────────┐
│       gRPC Server with Validation                │
│                                                   │
│  • ValidationInterceptor checks all RPCs         │
│  • Parses method name: /Service/Method           │
│  • Queries Registry for validation               │
└───────────────────┬──────────────────────────────┘
                    │
                    ▼
┌──────────────────────────────────────────────────┐
│            Registry Server                        │
│                                                   │
│  • ValidateMethod(namespace, service, method)     │
│  • LookupService(namespace, service)              │
│  • RegisterService(...)                           │
│  • ListServices(namespace)                        │
└───────────────────┬──────────────────────────────┘
                    │
                    ▼
┌──────────────────────────────────────────────────┐
│         Collection Storage                        │
│                                                   │
│  • RegisteredProtos collection                    │
│  • RegisteredServices collection                  │
└──────────────────────────────────────────────────┘
                    │
         ┌──────────┴──────────┐
         │                     │
      [Allow]              [Reject]
   Execute RPC       Return Unimplemented
```

## Core Concepts

### Service Registration

Services must be registered before their RPCs can be invoked:

```go
// Register CollectionService
err := registry.RegisterCollectionService(ctx, registryServer, "production")

// Register CollectiveDispatcher
err := registry.RegisterDispatcherService(ctx, registryServer, "production")

// Register CollectionRepo
err := registry.RegisterCollectionRepoService(ctx, registryServer, "production")
```

### Namespace Isolation

All registrations are scoped to namespaces:
- Different tenants can have different available services
- Dev/staging/prod can have different service registrations
- Feature flags can enable/disable services per namespace
- Multiple versions can coexist in different namespaces

### Validation Interceptors

gRPC interceptors automatically validate incoming RPCs:

```go
// Create server with validation
grpcServer := registry.NewServerWithValidation(registryServer, namespace)

// Register services
pb.RegisterCollectionServiceServer(grpcServer, collectionServer)

// All RPCs are now validated automatically
```

## Service-to-Service Communication

**Critical**: Services on the same gRPC server communicate via **loopback gRPC connections**, not direct method calls. This ensures proper gRPC stack usage and server wiring validation.

### The Problem with Direct Calls

If services call each other directly (bypassing gRPC):
- ❌ No validation of gRPC wiring
- ❌ Inconsistent with remote calls
- ❌ Missing gRPC layer benefits (interceptors, middleware)

### The Solution: Loopback gRPC

Services use loopback connections even when co-located:

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

### Implementation

```go
// 1. Start gRPC server with all services
grpcServer := registry.NewServerWithValidation(registryServer, namespace)
pb.RegisterCollectorRegistryServer(grpcServer, registryServer)
pb.RegisterCollectionServiceServer(grpcServer, collectionServer)

lis, _ := net.Listen("tcp", "localhost:50051")
go grpcServer.Serve(lis)

// 2. Create loopback gRPC connection to our own server
loopbackConn, _ := grpc.NewClient("localhost:50051",
    grpc.WithTransportCredentials(insecure.NewCredentials()))

// 3. Create Registry client via loopback
registryClient := pb.NewCollectorRegistryClient(loopbackConn)

// 4. Wrap client to implement validation interface
grpcValidator := &grpcRegistryClientValidator{client: registryClient}
validator := registry.NewGRPCRegistryValidator(grpcValidator)

// 5. Dispatcher uses gRPC-based validation
dispatcher := dispatch.NewDispatcherWithRegistry(collectorID, addr, namespaces, validator)
```

### Benefits

✅ **Proper gRPC Communication**
- Services communicate via gRPC even when co-located
- Full gRPC stack is exercised (marshaling, interceptors, etc.)

✅ **Validates Server Wiring**
- If Registry isn't properly registered, gRPC calls fail immediately
- Catches misconfiguration at startup

✅ **Consistent Behavior**
- Same code path for local and remote service calls
- No special cases for co-located services

✅ **gRPC Layer Features**
- Interceptors run for all calls
- Middleware applies consistently
- Proper error handling via status codes

## Basic Usage

### 1. Setting up the Registry

```go
import (
    "github.com/accretional/collector/pkg/registry"
    "github.com/accretional/collector/pkg/collection"
)

// Create collections to store registered protos and services
registeredProtos, _ := collection.NewCollection(
    &pb.Collection{Namespace: "system", Name: "registered_protos"},
    protosStore,
    &collection.LocalFileSystem{},
)

registeredServices, _ := collection.NewCollection(
    &pb.Collection{Namespace: "system", Name: "registered_services"},
    servicesStore,
    &collection.LocalFileSystem{},
)

// Create registry server
registryServer := registry.NewRegistryServer(registeredProtos, registeredServices)
```

### 2. Registering Services

```go
ctx := context.Background()
namespace := "production"

// Register all built-in services
err := registry.RegisterCollectionService(ctx, registryServer, namespace)
err := registry.RegisterDispatcherService(ctx, registryServer, namespace)
err := registry.RegisterCollectionRepoService(ctx, registryServer, namespace)
```

### 3. Creating Servers with Validation

Easiest way using helper functions:

```go
// Set up CollectionService with validation
grpcServer, listener, err := registry.SetupCollectionServiceWithValidation(
    ctx,
    registryServer,
    namespace,
    collectionRepo,
    "localhost:50051",
)

// Start the server
go grpcServer.Serve(listener)
```

Or manually:

```go
// Create server with validation interceptors
grpcServer := registry.NewServerWithValidation(registryServer, namespace)

// Register your service implementation
pb.RegisterCollectionServiceServer(grpcServer, collectionServer)

// Start server
lis, _ := net.Listen("tcp", ":50051")
grpcServer.Serve(lis)
```

## API Reference

### Registration RPCs

```go
// RegisterProto registers a protobuf file descriptor
resp, err := registryClient.RegisterProto(ctx, &pb.RegisterProtoRequest{
    Namespace:      "production",
    FileDescriptor: fileDesc,
    Dependencies:   []FileDescriptorProto{...},
})

// RegisterService registers a gRPC service
resp, err := registryClient.RegisterService(ctx, &pb.RegisterServiceRequest{
    Namespace:         "production",
    ServiceDescriptor: serviceDesc,
    FileDescriptor:    fileDesc,
})
```

### Query RPCs

```go
// LookupService retrieves a registered service
resp, err := registryClient.LookupService(ctx, &pb.LookupServiceRequest{
    Namespace:   "production",
    ServiceName: "CollectionService",
})

// ValidateMethod checks if a method is registered
resp, err := registryClient.ValidateMethod(ctx, &pb.ValidateMethodRequest{
    Namespace:   "production",
    ServiceName: "CollectionService",
    MethodName:  "Create",
})

// ListServices returns all registered services
resp, err := registryClient.ListServices(ctx, &pb.ListServicesRequest{
    Namespace: "production",  // Empty for all namespaces
})
```

### Validation Interface

For service implementations needing validation:

```go
// GRPCRegistryValidator validates via gRPC (recommended)
validator := registry.NewGRPCRegistryValidator(grpcValidator)

// Validate a service method
err := validator.ValidateServiceMethod(ctx, "production", "CollectionService", "Create")
if err != nil {
    // Method not registered
}
```

### Helper Functions

```go
// Create validator for direct use
validator := registry.NewRegistryValidator(registryServer)

// Get server options for adding validation
opts := registry.WithValidation(registryServer, "production")
grpcServer := grpc.NewServer(opts...)

// Create server with validation in one call
grpcServer := registry.NewServerWithValidation(registryServer, "production", extraOpts...)
```

## How Validation Works

### Interceptor Flow

```
1. Client makes RPC call: /CollectionService/Create

2. gRPC server receives request

3. ValidationInterceptor executes:
   │
   ├─> Parse method: "/CollectionService/Create"
   │   └─> Extract: service="CollectionService", method="Create"
   │
   ├─> Query Registry:
   │   └─> ValidateMethod(namespace, "CollectionService", "Create")
   │
   ├─> Decision:
   │   ├─> If registered: Allow request to proceed
   │   └─> If not registered: Return codes.Unimplemented
   │
   └─> Continue to handler or return error

4. If allowed, actual service handler executes

5. Response returned to client
```

### Validation Logic

```go
func (r *RegistryServer) ValidationInterceptor(namespace string) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        // Parse method name: /Service/Method
        parts := strings.Split(info.FullMethod, "/")
        serviceName := parts[1]
        methodName := parts[2]

        // Validate against registry
        err := r.ValidateMethod(ctx, namespace, serviceName, methodName)
        if err != nil {
            return nil, status.Errorf(codes.Unimplemented,
                "method %s.%s not registered in namespace %s",
                serviceName, methodName, namespace)
        }

        // Method is registered, proceed
        return handler(ctx, req)
    }
}
```

## Complete Example

```go
package main

import (
    "context"
    "net"

    "github.com/accretional/collector/pkg/collection"
    "github.com/accretional/collector/pkg/dispatch"
    "github.com/accretional/collector/pkg/registry"
    pb "github.com/accretional/collector/gen/collector"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func main() {
    ctx := context.Background()
    namespace := "production"

    // ================================================================
    // 1. Setup Registry Collections
    // ================================================================

    protosStore, _ := sqlite.NewSqliteStore("./data/protos.db", collection.Options{EnableJSON: true})
    servicesStore, _ := sqlite.NewSqliteStore("./data/services.db", collection.Options{EnableJSON: true})

    registeredProtos, _ := collection.NewCollection(
        &pb.Collection{Namespace: "system", Name: "registered_protos"},
        protosStore,
        &collection.LocalFileSystem{},
    )

    registeredServices, _ := collection.NewCollection(
        &pb.Collection{Namespace: "system", Name: "registered_services"},
        servicesStore,
        &collection.LocalFileSystem{},
    )

    // Create registry server
    registryServer := registry.NewRegistryServer(registeredProtos, registeredServices)

    // Register all services in the registry
    registry.RegisterCollectionService(ctx, registryServer, namespace)
    registry.RegisterDispatcherService(ctx, registryServer, namespace)
    registry.RegisterCollectionRepoService(ctx, registryServer, namespace)

    // ================================================================
    // 2. Create Single gRPC Server with ALL Services
    // ================================================================

    grpcServer := registry.NewServerWithValidation(registryServer, namespace)

    // Register ALL services on the same server
    pb.RegisterCollectorRegistryServer(grpcServer, registryServer)

    collectionServer := collection.NewCollectionServer(collectionRepo)
    pb.RegisterCollectionServiceServer(grpcServer, collectionServer)

    repoGrpcServer := collection.NewGrpcServer(collectionRepo)
    pb.RegisterCollectionRepoServer(grpcServer, repoGrpcServer)

    // ================================================================
    // 3. Start Server and Create Loopback Connection
    // ================================================================

    lis, _ := net.Listen("tcp", "localhost:50051")
    go grpcServer.Serve(lis)
    time.Sleep(100 * time.Millisecond) // Let server start

    // Create loopback connection for service-to-service communication
    loopbackConn, _ := grpc.NewClient("localhost:50051",
        grpc.WithTransportCredentials(insecure.NewCredentials()))

    // Create Registry client via loopback
    registryClient := pb.NewCollectorRegistryClient(loopbackConn)

    // Wrap for validation interface
    grpcValidator := &grpcRegistryClientValidator{client: registryClient}
    validator := registry.NewGRPCRegistryValidator(grpcValidator)

    // ================================================================
    // 4. Create Dispatcher with gRPC-based Registry Validation
    // ================================================================

    dispatcher := dispatch.NewDispatcherWithRegistry(
        "collector-001",
        "localhost:50051",
        []string{namespace},
        validator, // Uses gRPC to validate!
    )

    // Register Dispatcher service
    pb.RegisterCollectiveDispatcherServer(grpcServer, dispatcher)

    log.Println("All services running with registry validation!")
    log.Println("  - CollectorRegistry")
    log.Println("  - CollectionService")
    log.Println("  - CollectiveDispatcher")
    log.Println("  - CollectionRepo")

    // Block forever
    select {}
}

// grpcRegistryClientValidator wraps gRPC client to implement ServiceMethodValidator
type grpcRegistryClientValidator struct {
    client pb.CollectorRegistryClient
}

func (v *grpcRegistryClientValidator) ValidateServiceMethod(
    ctx context.Context, namespace, serviceName, methodName string,
) error {
    // Validate by calling Registry via gRPC
    serviceDesc := &pb.RegisterServiceRequest{
        Namespace: namespace,
        ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
            Name: proto.String(serviceName),
        },
    }

    _, err := v.client.RegisterService(ctx, serviceDesc)
    if err != nil {
        // AlreadyExists means service is registered - validation passes!
        if grpc.Code(err).String() == "AlreadyExists" {
            return nil
        }
        return fmt.Errorf("service not registered: %w", err)
    }

    // If registration succeeded, service wasn't registered before
    return fmt.Errorf("service %s.%s not registered in namespace %s",
        serviceName, methodName, namespace)
}
```

## Lookup Functions

### Server-Side Lookup

```go
// LookupProto retrieves a registered proto by namespace and file name
proto, err := registryServer.LookupProto(ctx, "production", "collection.proto")

// LookupService retrieves a registered service
service, err := registryServer.LookupService(ctx, "production", "CollectionService")

// ValidateService checks if a service is registered
err := registryServer.ValidateService(ctx, "production", "CollectionService")

// ValidateMethod checks if a method exists on a service
err := registryServer.ValidateMethod(ctx, "production", "CollectionService", "Create")

// ListProtos returns all registered protos (optionally filtered by namespace)
protos, err := registryServer.ListProtos(ctx, "production")

// ListServices returns all registered services (optionally filtered by namespace)
services, err := registryServer.ListServices(ctx, "production")
```

### Dynamic Service Discovery

```go
// List all services in a namespace
services, err := registryServer.ListServices(ctx, "production")

for _, service := range services {
    fmt.Printf("Service: %s/%s\n", service.Namespace, service.ServiceName)
    fmt.Printf("Methods: %v\n", service.MethodNames)

    // Access full service descriptor
    descriptor := service.ServiceDescriptor
}

// Query specific service
service, err := registryServer.LookupService(ctx, "production", "CollectionService")

// Check method availability
for _, method := range service.MethodNames {
    if method == "Create" {
        // Method is available
    }
}
```

## Testing

The registry package includes comprehensive tests:

```bash
go test ./pkg/registry/... -v
```

**Test Files:**
- `registry_test.go`: Registration and lookup tests
- `interceptor_test.go`: Validation interceptor tests
- `integration_test.go`: End-to-end integration tests

**Test Coverage:**
- Service registration (duplicate handling, nil values, empty namespace)
- Proto registration (dependencies, multiple messages, complex types)
- Lookup functionality (found/not found cases)
- Validation (service exists, method exists)
- Interceptors (valid/invalid RPCs, streaming)
- Multi-namespace isolation
- Dynamic service discovery

## Data Model

### RegisteredProto

```protobuf
message RegisteredProto {
  string id = 1;  // namespace/message_name
  string namespace = 2;
  repeated string message_names = 3;
  google.protobuf.FileDescriptorProto file_descriptor = 4;
  repeated string dependencies = 5;  // Dependency IDs
  Metadata metadata = 6;
}
```

### RegisteredService

```protobuf
message RegisteredService {
  string id = 1;  // namespace/service_name
  string namespace = 2;
  string service_name = 3;
  google.protobuf.ServiceDescriptorProto service_descriptor = 4;
  repeated string method_names = 5;
  Metadata metadata = 6;
}
```

## Performance

Loopback gRPC calls are fast:
- Localhost TCP has minimal overhead
- No network serialization for simple types
- OS kernel optimizes localhost connections
- Typical latency: <1ms

**Performance Comparison:**
- Direct method call: ~10ns
- Localhost gRPC call: ~100μs-1ms
- Remote gRPC call: ~10-100ms

The slight overhead of loopback is worth it for:
- Correctness (proper gRPC stack usage)
- Consistency (same code path as remote calls)
- Validation (ensures server wiring is correct)

## Best Practices

1. **Register services early**: Register all services during startup before accepting requests
2. **Use namespaces**: Isolate services by environment, tenant, or version
3. **Check errors**: Always check registration errors to ensure services are properly registered
4. **Test validation**: Write tests that verify unregistered methods are rejected
5. **Use loopback for same-server calls**: Even co-located services should communicate via gRPC
6. **Monitor registrations**: Track which services are registered in each namespace
7. **Document service contracts**: Include service documentation in proto files

## Future Enhancements

- HTTP/REST endpoint for querying the registry
- Proto file upload/download via API
- Service versioning and deprecation tracking
- Auto-registration from proto file reflection
- Registry replication across collectors
- Web UI for browsing registered services
- Dedicated `ValidateMethod` RPC (instead of using RegisterService + AlreadyExists)
- Caching of validation results
- Health checks via loopback connection
