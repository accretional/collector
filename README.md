# Collector

Collector is a grpc + proto framework that makes heavy use of reflection, an ORM (sqlite + common CRUD API for proto "Collections"), a proto/grpc/API registry, a "dynamic"/"functional" distributed rpc system, a distributed programming platform, and a control plane console.  It can be run alone, or in a distributed system. 

You can register and update protobuf messages and grpc services in Collector's registry, then create Collections (tables+API server) of the new protos that can be managed through standard Create, Get, Update, Delete, List, and 💥Search💥 APIs, plus custom grpc methods.

Allowing clients to create, define, and call custom grpc methods and store data in arbitrary tables on your server is somewhat dangerous. But that's what makes it so powerful!

Collector allows you to define a custom Serve method that wraps dynamic grpc method calls so you can run them somewhere more safely. We recommend sandboxed execution environments or containers.

Reflections can reflect reflections that call APIs that reflect into tables of new reflections. Dynamically dispatched RPCs can radiate into new protobufs and workflow configurations that are dynamically dispatched themselves upon creation. 

Is that anxiety you felt, or a gnawing hunger awakened by the sight of a cutting edge distributed programming framework? 

## LLMs, Agents and You

Is collector for LLMs, distributed sytems, or humans? Yes. It's for all of them.

Stay tuned for more information. As you might guess, a distributed system for dynamic dispatch, discovery, storage, and registration of strongly-typed RPCs/Messages/Workflows into tables and sandbox environments is agent-friendly. But it's built for much more than that, too.

# Design

Everything in Collector is namespaced, besides the top-level entities implementing those namespaces. In the data layer, this essentially means that each namespace has a separate directory from others at the same level of scope. At the API layer, it means that we use namespace, name, and URI (may also encode sub-structure) fields as identifiers quite a lot.

The most fundamental datastructure to Collector is the Collection. One part of it is the sqlite table of named protobufs, indexed by name but searchable via sqlite's JSONB filtering. These protobufs may have data uris corresponding to data stored elsewhere, but their protobufs/configs remains in the Collection. A Collection can also have a hierarchical arrange of data in a more filesytem-liked structure, called CollectionDirs and containing CollectionData. Generally, we reocmmend thinking of the Collection and its tables a kind of "inode" at the root of a hierarchical filesystem consisting of CollectionData. Like an inode, it literally indexes and provides metadata for the rest of the files, and it is key to navigating them.

Next is the CollectionServer. This is an actual "loaded" Collection serving CRUD, search, and custom RPCs. As much as possible, the rest of the internals of Collector try to leverage CollectionServer to reduce the size and complexity of its interfaces/state. This is what we intend for humans to use it for too, of course. Because CollectionServer may be running untrusted code, CollectionServer does introduce a Call RPC but delegates its actual implementation to DynamicDispatcher's Serve. It also implements a Search API based on sqlite's jsonb indexing and grpc's JSON encoding.

DynamicDispatcher has to have its Serve functionality configured or implemented specially to maintain security. It uses the Connect API to open DynamicDispatcherServers to other entities and Dispatch to begin dispatching something to a DynamicDispatcherServer within Serve. The Dispatch protobuf is a general model for contextualized RPCs between Collector instances. DynamicDispatcher maintains four collections: DispatchServers, Connections, Dispatches, and Collectors (cluster peers/indirect peers).

CollectionRepo is the system for managing and using individual/aggregate Collections, and answering search queries across Collections. It is a Collection of CollectionServers (the table) and their Collections (the files), containing both "system" Collections and those created by clients. It implements the CreateCollection, Discover, Route, and SearchCollections APIs.

CollectiveWorker is a workflow system for aggregated or iterated RPCs across a cluster of Collectors. Three collections: WorkflowDefinitions, ActiveWorkflows, Executions. Has a notion of subtypes of Workflows: Tasks (non-root nodes in a workflow), Continuations (callbacks that can loop), Invocations, Executions(history). Has StartWorkflow, QueryWorkflowStatus, and GetWorkflowHistroy APIs.

CollectorConsole implements some UI/analysis/helper APIs for visualizing the system, debugging it, and configuring it. It does cool stuff with reflection.

CollectorRegistry is what allows new protomessages and grpc services to be registered via golang's [proto registries](https://pkg.go.dev/google.golang.org/protobuf/reflect/protoregistry) registries, as trees of files under a specified path.

# CollectiveDispatcher: Distributed RPC Routing

The CollectiveDispatcher (`pkg/dispatch`) enables transparent distributed RPC execution across a cluster of Collector instances. It provides three core RPCs that work together to create a dynamic, namespace-aware routing mesh:

## The Three RPCs

### 1. **Connect** - Establish Collector-to-Collector Links
Collectors call `Connect` to establish bidirectional communication channels. When two collectors connect:
- They exchange their supported namespaces
- The system computes **shared namespaces** (intersection of both collectors' namespaces)
- Connection metadata is stored on both sides with proper collector IDs
- Each collector can now route requests to the other

**Example:**
```go
// Collector1 connects to Collector2
resp, err := collector1.ConnectTo(ctx, "collector2:50051", []string{"users", "orders"})
// If Collector2 handles ["orders", "products"], shared namespace is ["orders"]
```

### 2. **Serve** - Execute Local Service Methods
`Serve` is the execution primitive. When a collector receives a Serve request:
- It looks up the registered handler for `namespace.service.method`
- Executes the handler with the provided input
- Returns the output and indicates which collector executed it (`ExecutorId`)

**Example:**
```go
// Register a service handler
dispatcher.RegisterService("users", "UserService", "GetUser",
    func(ctx context.Context, input interface{}) (interface{}, error) {
        // Your implementation here
        return userData, nil
    })

// Serve RPC executes the handler
resp, err := client.Serve(ctx, &ServeRequest{
    Namespace:  "users",
    Service:    &ServiceTypeRef{ServiceName: "UserService"},
    MethodName: "GetUser",
    Input:      userID,
})
```

### 3. **Dispatch** - Smart Request Routing
`Dispatch` is the high-level API that combines connection topology with service execution. It supports two routing modes:

#### **Target-Specific Routing**
Explicitly route to a specific collector by ID:
```go
resp, err := client.Dispatch(ctx, &DispatchRequest{
    Namespace:         "orders",
    Service:           &ServiceTypeRef{ServiceName: "OrderService"},
    MethodName:        "CreateOrder",
    Input:             orderData,
    TargetCollectorId: "collector-west-2",  // Route to specific collector
})
```

#### **Auto-Routing**
Let the dispatcher find an appropriate collector:
1. **Try local first**: If the method is registered locally, execute it
2. **Route to connected collector**: Find a connection with the shared namespace
3. **Transparent proxying**: Forward via `Serve` RPC to the remote collector

```go
// No target specified - dispatcher finds the right collector automatically
resp, err := client.Dispatch(ctx, &DispatchRequest{
    Namespace:  "orders",
    Service:    &ServiceTypeRef{ServiceName: "OrderService"},
    MethodName: "CreateOrder",
    Input:      orderData,
    // TargetCollectorId left empty = auto-route
})
// Returns resp.HandledByCollectorId indicating which collector handled it
```

## Complete Example: Multi-Hop RPC Execution

Here's how a request flows through the dispatcher mesh:

```
┌─────────┐                ┌────────────┐                ┌────────────┐
│ Client  │                │ Collector1 │                │ Collector2 │
│         │                │  (ns: us)  │                │  (ns: eu)  │
└────┬────┘                └─────┬──────┘                └─────┬──────┘
     │                           │                             │
     │ 1. Dispatch               │                             │
     │    {namespace: "eu"}      │                             │
     ├──────────────────────────►│                             │
     │                           │                             │
     │                           │ 2. Check local: Not found   │
     │                           │                             │
     │                           │ 3. Check connections:       │
     │                           │    Found Collector2         │
     │                           │    with shared namespace    │
     │                           │                             │
     │                           │ 4. Serve RPC                │
     │                           ├────────────────────────────►│
     │                           │    {namespace: "eu"}        │
     │                           │                             │
     │                           │                             │ 5. Execute
     │                           │                             │    handler
     │                           │                             │
     │                           │ 6. ServeResponse            │
     │                           │◄────────────────────────────┤
     │                           │    {ExecutorId: "coll2"}    │
     │                           │                             │
     │ 7. DispatchResponse       │                             │
     │◄──────────────────────────┤                             │
     │  {HandledByCollectorId:   │                             │
     │   "collector2"}           │                             │
     │                           │                             │
```

### Code Example:

```go
// Setup Collector1
dispatcher1 := dispatch.NewDispatcher("collector1", "localhost:50051", []string{"users", "orders"})
server1 := grpc.NewServer()
pb.RegisterCollectiveDispatcherServer(server1, dispatcher1)

// Setup Collector2 with a service handler
dispatcher2 := dispatch.NewDispatcher("collector2", "localhost:50052", []string{"orders", "products"})
dispatcher2.RegisterService("orders", "OrderService", "CreateOrder",
    func(ctx context.Context, input interface{}) (interface{}, error) {
        // Process order
        return orderResult, nil
    })
server2 := grpc.NewServer()
pb.RegisterCollectiveDispatcherServer(server2, dispatcher2)

// Establish connection
dispatcher1.ConnectTo(ctx, "localhost:50052", []string{"users", "orders"})

// Client dispatches to Collector1
client := pb.NewCollectiveDispatcherClient(conn_to_collector1)
resp, err := client.Dispatch(ctx, &DispatchRequest{
    Namespace:  "orders",
    Service:    &ServiceTypeRef{ServiceName: "OrderService"},
    MethodName: "CreateOrder",
    Input:      orderInput,
    // No target specified - auto-routes to Collector2!
})

// resp.HandledByCollectorId == "collector2"
// The request was transparently routed and executed!
```

## Key Features

### Namespace-Based Routing
- Collectors advertise which namespaces they support
- Connections track **shared namespaces** (intersection)
- Auto-routing only considers collectors with the target namespace

### Transparent Proxying
- `Dispatch` → `Serve` conversion happens automatically
- Clients don't need to know the cluster topology
- Results include `HandledByCollectorId` for observability

### Bidirectional Connections
- Both collectors can route to each other after connecting
- Supports mesh topologies, not just hub-and-spoke

### Service Registry
- Register handlers dynamically: `RegisterService(namespace, service, method, handler)`
- Handlers are simple Go functions: `func(context.Context, interface{}) (interface{}, error)`
- Multiple services per namespace supported

## Architecture Benefits

1. **Location Transparency**: Services can move between collectors without client changes
2. **Dynamic Discovery**: New collectors can join and handle requests immediately after connecting
3. **Load Distribution**: Multiple collectors can handle the same namespace
4. **Fault Tolerance**: If one collector fails, requests auto-route to others with the same namespace
5. **Simple Programming Model**: Register a handler, connect, dispatch - that's it!

## Testing

The dispatcher has comprehensive test coverage in `pkg/dispatch/dispatcher_test.go`:
- 7 connection tests (basic, bidirectional, multiple, shared namespaces, error handling, real network)
- 4 serve tests (invocation, error handling, invalid requests, multiple services)
- 5 dispatch tests (specific target, local routing, remote routing, error cases)

All tests use both in-memory (`bufconn`) and real network connections to verify correct behavior.
