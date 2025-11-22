# Registry Package

The registry package provides a centralized service registry for the collector system. It allows you to register protobuf types and gRPC services, validate RPC calls against registered types, and dynamically discover available services.

## Features

- **Service Registration**: Register gRPC services with their method signatures
- **Proto Registration**: Register protobuf file descriptors and message types
- **RPC Validation**: Automatically validate incoming RPC calls against registered services
- **Dynamic Lookup**: Query registered services and their methods at runtime
- **Namespace Isolation**: Services can be registered in different namespaces for multi-tenancy

## Basic Usage

### 1. Setting up the Registry

```go
import (
    "github.com/accretional/collector/pkg/registry"
    "github.com/accretional/collector/pkg/collection"
)

// Create collections to store registered protos and services
registeredProtos, _ := collection.NewCollection(...)
registeredServices, _ := collection.NewCollection(...)

// Create registry server
registryServer := registry.NewRegistryServer(registeredProtos, registeredServices)
```

### 2. Registering Services

The registry provides helper functions to register the built-in services:

```go
ctx := context.Background()
namespace := "production"

// Register CollectionService
err := registry.RegisterCollectionService(ctx, registryServer, namespace)

// Register CollectiveDispatcher
err := registry.RegisterDispatcherService(ctx, registryServer, namespace)

// Register CollectionRepo
err := registry.RegisterCollectionRepoService(ctx, registryServer, namespace)
```

### 3. Creating Servers with Validation

The easiest way to add validation is using the setup functions:

```go
import pb "github.com/accretional/collector/gen/collector"

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

Or manually add validation to an existing server:

```go
// Create server with validation interceptors
grpcServer := registry.NewServerWithValidation(registryServer, namespace)

// Register your service implementation
pb.RegisterCollectionServiceServer(grpcServer, collectionServer)
```

## API Reference

### Lookup Functions

```go
// LookupProto retrieves a registered proto by namespace and file name
proto, err := registryServer.LookupProto(ctx, "production", "collection.proto")

// LookupService retrieves a registered service by namespace and service name
service, err := registryServer.LookupService(ctx, "production", "CollectionService")

// ListProtos returns all registered protos (optionally filtered by namespace)
protos, err := registryServer.ListProtos(ctx, "production")

// ListServices returns all registered services (optionally filtered by namespace)
services, err := registryServer.ListServices(ctx, "production")
```

### Validation Functions

```go
// ValidateService checks if a service is registered
err := registryServer.ValidateService(ctx, "production", "CollectionService")

// ValidateMethod checks if a method exists on a service
err := registryServer.ValidateMethod(ctx, "production", "CollectionService", "Create")
```

### Helper Functions

```go
// Create validator for use in service implementations
validator := registry.NewRegistryValidator(registryServer)

// Get server options for adding validation
opts := registry.WithValidation(registryServer, "production")
grpcServer := grpc.NewServer(opts...)
```

## How Validation Works

When a gRPC server is created with validation:

1. The validation interceptor examines incoming RPC calls
2. It extracts the service name and method name from the request
3. It checks the registry to verify the method is registered in the specified namespace
4. If registered, the request proceeds normally
5. If not registered, the request is rejected with `codes.Unimplemented`

This ensures that only services and methods that have been explicitly registered can be invoked.

## Namespace Design

Services are registered per namespace, allowing:

- **Multi-tenancy**: Different tenants can have different available services
- **Environment isolation**: Dev/staging/prod can have different service registrations
- **Feature flags**: Enable/disable services per namespace
- **Version management**: Multiple versions of a service in different namespaces

Example:

```go
// Register v1 in production
registry.RegisterCollectionService(ctx, registryServer, "production")

// Register v2 in staging
registry.RegisterCollectionServiceV2(ctx, registryServer, "staging")

// Each namespace validates against its own registry
prodServer := registry.NewServerWithValidation(registryServer, "production")
stagingServer := registry.NewServerWithValidation(registryServer, "staging")
```

## Dynamic Service Discovery

List all services available in a namespace:

```go
services, err := registryServer.ListServices(ctx, "production")

for _, service := range services {
    fmt.Printf("Service: %s/%s\n", service.Namespace, service.ServiceName)
    fmt.Printf("Methods: %v\n", service.MethodNames)

    // Access full service descriptor
    descriptor := service.ServiceDescriptor
}
```

Query specific service information:

```go
service, err := registryServer.LookupService(ctx, "production", "CollectionService")

// Check if method exists
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

Key test files:
- `registry_test.go`: Tests for registration and lookup
- `interceptor_test.go`: Tests for validation interceptors
- `integration_test.go`: End-to-end integration tests

## Architecture

```
┌─────────────────┐
│  gRPC Server    │
│  with           │
│  Interceptor    │
└────────┬────────┘
         │
         │ Validates
         ▼
┌─────────────────┐
│ Registry Server │
│                 │
│ - Lookup        │
│ - Validate      │
└────────┬────────┘
         │
         │ Stores in
         ▼
┌─────────────────┐
│  Collections    │
│                 │
│ - Protos        │
│ - Services      │
└─────────────────┘
```

## Best Practices

1. **Register services early**: Register all services during startup before accepting requests
2. **Use namespaces**: Isolate services by environment, tenant, or version
3. **Check errors**: Always check registration errors to ensure services are properly registered
4. **Test validation**: Write tests that verify unregistered methods are rejected
5. **Monitor registrations**: Track which services are registered in each namespace

## Future Enhancements

Potential additions:
- HTTP/REST endpoint for querying the registry
- Proto file upload/download
- Service versioning and deprecation
- Auto-registration from proto files
- Registry replication across collectors
- Web UI for browsing registered services
