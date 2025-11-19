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

# Fundamentals

Collector is the name for the bundle of grpc services that work together using reflection, proto registries, grpc, and other [magic](https://pkg.go.dev/google.golang.org/protobuf@v1.36.10/reflect/protodesc).

* CollectorRegistry is the system for storing proto messages and grpc service definitions in namespaced [proto registries](https://pkg.go.dev/google.golang.org/protobuf/reflect/protoregistry), as trees of files under a specified path.
* CollectionRepo is the system for managing and using Collections (sqlite tables containing protobufs of a common type) to store data, and for aggregate operations or queries across Colections. Collections are exposed to clients under CollectionServers.
* CollectionServer is the per-Collection "ORM" implementing common API calls, as well as a Search api based on sqlite's jsonb indexing and grpc's JSON encoding. It also allows CollectedMessages to be called with custom and dynamic grpc methods registered in the same namespace for their particular types.
* CollectiveDispatcher implements the powerful Serve, Connect, and Dispatch APIs for dynamic/distributed dispatch of custom RPCs' execution, or multi-Collector systems.
* CollectiveWorker implements the Workflow, Task, Continuation, Invocation, and Execution APIs for coordinating long-running and chained work spanning multiple CollectiveDispatcher RPCs. Default functionality includes the ability to compile golang code into grpc servers (kinda).
* CollectorConsole implements some UI/analysis/helper APIs for visualizing the system, debugging it, and configuring it.
