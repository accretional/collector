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
