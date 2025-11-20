# Dispatcher Package

The `dispatcher` package implements the `DynamicDispatcher` component for the Collector framework. This component provides the basic `Serve` functionality that executes gRPC requests after receiving serialized gRPC method calls from other Dispatcher instances.

## Overview

The `DynamicDispatcher` implements the `CollectiveDispatcher` service defined in `proto/dispatcher.proto` and provides:

- **Serve**: Executes gRPC requests dynamically based on serialized method information
- **Connect**: (placeholder) Establishes connections to other dispatcher instances
- **Dispatch**: (placeholder) Dispatches requests to remote dispatcher instances

## Basic Usage

```go
import "github.com/accretional/collector/pkg/dispatcher"

// Create a new dispatcher with default config (direct execution)
d := dispatcher.New()

// Or create with custom configuration
config := &dispatcher.ExecutorConfig{
    Mode:           dispatcher.ExecutionModeContainer, // Use container execution
    Timeout:        30 * time.Second,
    ContainerImage: "golang:1.21-alpine",
    MaxMemory:      "512m",
    MaxCPU:         "1",
}
d = dispatcher.NewWithConfig(config)

// Create a serve request with service binary
req := &pb.ServeRequest{
    Namespace: "example",
    Service: &pb.ServiceTypeRef{
        Namespace:   "example",
        ServiceName: "GreeterService",
    },
    MethodName: "SayHello",
    Input:      inputAny, // protobuf Any containing request data
    ServiceDef: &pb.ServeRequest_ServiceBinary{
        ServiceBinary: serviceBinaryData, // Actual executable binary
    },
}

// Execute the request
resp, err := d.Serve(context.Background(), req)
if err != nil {
    // Handle error
}

// Check response status
if resp.Status.Code == pb.Status_OK {
    // Success - resp.Output contains the result
}
```

## Current Implementation Status

### ✅ Implemented
- Basic `Serve` method with validation and error handling
- **Two execution modes**: Direct (unsafe/fast) and Container (sandboxed)
- **Direct execution**: Uses go-memexec to execute binary data from memory
- **Container execution**: Runs service binaries in sandboxed Docker containers
- Service binary and URI support in ServeRequest (oneof service_def)
- Service and method registry structure (fallback mode)
- gRPC status code conversion
- Comprehensive test coverage
- Input validation for required fields

### 🚧 Placeholder/Future Work
- Service URI resolution and execution
- Service registration and discovery
- `Connect` method implementation
- `Dispatch` method implementation
- Enhanced security features and resource limits

## Architecture

The `DynamicDispatcher` supports multiple execution modes:

### 1. Binary Execution (Primary)
When `service_binary` is provided in the ServeRequest:
- **Direct Mode**: Uses `go-memexec` to execute binary data directly from memory
- **Container Mode**: Executes binary in a sandboxed Docker container with resource limits

### 2. URI Resolution (Future)
When `service_uri` is provided, the dispatcher will resolve and fetch the service implementation.

### 3. Registry-based (Fallback)
Maintains two internal registries for backwards compatibility:
- `serviceRegistry`: Maps namespace+service keys to service implementations
- `methodRegistry`: Maps service+method keys to gRPC method descriptors

### Execution Flow
1. Validate request fields (namespace, service, method_name)
2. Check for service_binary → execute directly or in container
3. Check for service_uri → resolve and execute (not yet implemented)
4. Fall back to registry lookup → execute via registered handlers

## Security Considerations

As mentioned in the main README, allowing dynamic execution of arbitrary gRPC methods is powerful but potentially dangerous. Future implementations should include:
- Sandboxed execution environments
- Permission and capability checking
- Resource limits and timeouts
- Audit logging

## Testing

Run tests with:
```bash
go test ./pkg/dispatcher/
```

For verbose output:
```bash
go test -v ./pkg/dispatcher/
```
