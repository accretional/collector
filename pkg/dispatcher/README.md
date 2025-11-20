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

// Create a new dispatcher
d := dispatcher.New()

// Create a serve request
req := &pb.ServeRequest{
    Namespace: "example",
    Service: &pb.ServiceTypeRef{
        Namespace:   "example",
        ServiceName: "GreeterService",
    },
    MethodName: "SayHello",
    Input:      inputAny, // protobuf Any containing request data
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
- Service and method registry structure
- gRPC status code conversion
- Comprehensive test coverage
- Input validation for required fields

### 🚧 Placeholder/Future Work
- Actual method execution (currently returns UNIMPLEMENTED)
- Service registration and discovery
- `Connect` method implementation
- `Dispatch` method implementation
- Security and sandboxing features mentioned in the design

## Architecture

The `DynamicDispatcher` maintains two internal registries:
- `serviceRegistry`: Maps namespace+service keys to service implementations
- `methodRegistry`: Maps service+method keys to gRPC method descriptors

This allows for dynamic registration and execution of gRPC services without compile-time binding.

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
