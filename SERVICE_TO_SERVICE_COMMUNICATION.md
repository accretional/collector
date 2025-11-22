# Service-to-Service Communication via gRPC

## The Problem

Initially, when services were co-located on the same gRPC server, the Dispatcher was calling the Registry via direct Go method calls, bypassing the gRPC layer entirely. This was problematic because:

1. **No validation of gRPC wiring** - Direct calls don't verify the gRPC server is properly configured
2. **Inconsistent with remote calls** - Remote collectors use gRPC, local should too
3. **Missing gRPC layer benefits** - No interceptors, no middleware, no proper RPC semantics

## The Solution: Loopback gRPC Connection

Now services communicate via gRPC even when on the same server, using a **loopback connection**.

### Architecture

```
┌─────────────────────────────────────────────────────┐
│          Single gRPC Server (port 50051)            │
│                                                     │
│  ┌──────────────┐         ┌──────────────┐        │
│  │  Dispatcher  │ ─────>  │   Registry   │        │
│  │              │  gRPC   │              │        │
│  └──────────────┘  call   └──────────────┘        │
│         │              via loopback connection     │
│         └──────────────────┐                       │
│                            ▼                       │
│                    localhost:50051                 │
└─────────────────────────────────────────────────────┘
                            │
                            │ (actual gRPC call)
                            ▼
                    gRPC validation interceptor
                            │
                            ▼
                    Registry.RegisterService()
```

### Implementation (cmd/server/main.go)

```go
// 1. Start gRPC server with all services
grpcServer := registry.NewServerWithValidation(registryServer, namespace)
pb.RegisterCollectorRegistryServer(grpcServer, registryServer)
// ... register other services ...

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

### How It Works

1. **Server starts** with all services registered
2. **Loopback connection** is established to `localhost:50051`
3. **Dispatcher validates** by making actual gRPC calls:
   ```go
   // This goes through the full gRPC stack!
   _, err := registryClient.RegisterService(ctx, serviceDesc)
   ```
4. **gRPC interceptors run** - validation, logging, metrics all apply
5. **Registry service executes** - same path as remote calls

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

### Validation Flow

When Dispatcher needs to validate a service:

```
Dispatcher.Serve()
   │
   ├─> validator.ValidateServiceMethod(namespace, service, method)
   │
   └─> grpcRegistryClientValidator.ValidateServiceMethod()
       │
       └─> registryClient.RegisterService() via gRPC
           │
           └─> localhost:50051 (loopback)
               │
               ├─> gRPC validation interceptor
               │
               └─> RegistryServer.RegisterService()
                   │
                   └─> Returns AlreadyExists if service registered ✓
```

### Why This Approach?

**Alternative 1: Direct method calls** (what we had before)
- ❌ Bypasses gRPC layer
- ❌ No validation of server wiring
- ❌ Inconsistent with remote calls

**Alternative 2: In-process gRPC channel**
- ⚠️ Complex setup
- ⚠️ Not standard gRPC practice
- ⚠️ Harder to test

**Alternative 3: Loopback connection** (what we use now)
- ✅ Standard gRPC practice
- ✅ Full gRPC stack exercised
- ✅ Easy to test and debug
- ✅ Minimal overhead (localhost has ~0 latency)

## Testing

The multi-collector integration test validates this works:

```go
// pkg/integration/multi_collector_test.go
func TestMultiCollectorIntegration(t *testing.T) {
    // Start collector 1 with all services
    grpcServer1, lis1, dispatcher1, _, _ := setupCollector(t, "collector-1", namespace, 0)
    go grpcServer1.Serve(lis1)

    // Dispatcher validates via gRPC to Registry on same server
    // This call goes through full gRPC stack!
    resp, _ := dispatcher1.Serve(ctx, &pb.ServeRequest{...})

    // ✓ Proves gRPC-based validation works
}
```

## Performance

Loopback gRPC calls are **fast**:
- Localhost TCP has minimal overhead
- No network serialization for simple types
- OS kernel optimizes localhost
- Typical latency: <1ms

For reference:
- Direct method call: ~10ns
- Localhost gRPC call: ~100μs-1ms
- Remote gRPC call: ~10-100ms

The tradeoff is worth it for correctness and consistency.

## Future Improvements

1. **Add dedicated ValidateMethod RPC**
   - Currently we check via RegisterService + AlreadyExists
   - Better: Add `ValidateMethod(namespace, service, method)` to Registry proto
   - Would be more explicit and efficient

2. **Caching**
   - Cache validation results for a few seconds
   - Reduce repeated gRPC calls for same service

3. **Health checks**
   - Use loopback connection for health monitoring
   - Verify all services are accessible via gRPC

## Summary

✅ **Services communicate via gRPC even when co-located**
✅ **Loopback connection ensures proper gRPC stack usage**
✅ **Validates server wiring at runtime**
✅ **Consistent behavior for local and remote calls**
✅ **Full gRPC features (interceptors, middleware) apply**

The system now has proper service-to-service communication through gRPC!
