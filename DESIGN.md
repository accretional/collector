# Collector Design v0.1

**Collector** is a general system for agentic computing, consisting of multiple "tiered" abstractions that build on each other—from a searchable data store (a Collection) up to a stateful, multi-tenant "agent mesh."

---

# Foreword

## 1. Technical Goals

Collector addresses a problem I've been ruminating about over the past several years: what technical infrastructure would best allow AI agents to operate autonomously at scale? I think it needs a very particular architecture:

### Identity & Hierarchy

* A **hierarchical registry** of identity/auth "namespaces" or tenancy units.
* Nested multitenancy with deep and flexible "scoping" of permissions from the root tenancy unit down to leaf nodes.
* Identity, authn, and authz should "just work."

### Data Topology & Structure

* A **"Unix-like" universal API** for *structured*, *semantic* data.
* A hierarchical registry of APIs and data structures.
* A **9P-like "everything is a file"** data topology. This implies a uniform interface for accessing local data via filesystem or remote data via network (isomorphism).
* Every "dataset" comes with all standard **CRUD APIs, plus search**.

### System Architecture

* A distributed system **"microkernel"** implementing core control plane, data plane, and config functionality in one compact service.
* A hierarchical index of actual data, API providers, and tenancy units in a multitenant distributed environment.
* **Introspection:** Every entity within the system is discoverable and uses reflection.
* **Dynamic Mutation:** Every data type can be mutated/processed via dynamically registered, persistent functions.

### Resource Management

* No compromises or leaky abstractions between "ephemeral, fungible, dynamically provisioned" resources and "persistent, irreplaceable" ones.
* Compute, disk, and network resources under normal workloads should be **serverless/dynamically provisioned** without the need for manual tuning.

---

## 2. The Agent Mesh

These requirements are inspired by the ways existing distributed systems fail to provide AI agents with the right structure for autonomous work.

**The User Story:**

> Agents should be able to be "dropped into" a compute environment knowing only a small set of core APIs, capable of writing programs that serialize/deserialize/process well-defined data structures according to well-defined function interfaces.
> They should possess enough memory or "working state" to craft and execute non-trivial plans, fully understand their environment, and drive the execution of goals at massive computational scale in coordination with others.

Despite the scope of the problem, we also need to keep things simple! **The project's technical requirements are constraints, not features.**  This is perhaps the most important and challenging part of the project.

---

## 3. A New Kind of Distributed System

Existing systems (like K8s or Service Meshes) understandably fail to deliver the right properties for agents because they treat resource provisioning, API integration, and tenancy units as **immutable at runtime**.

* **Eliminating Friction** Humans typically prefer friction and out-of-band configuration to ensure certain changes only occur under direct manual supervision, as these changes involve cost or security implications.
* **Flexible Guardrails:** LLMs capable of writing and calling APIs need flexibility. While humans set guardrails (budgets, allow/disallow lists), agents need to provision resources and coordinate autonomously without direct orchestration.

Although agents may make non-trivial infrastructure decisions, the resources they use might be completely ephemeral. Alternatively, they may generate massive amounts of useful data that humans want to keep but cannot easily organize themselves. Perhaps they can accomplish their task by finding and using existing data, producing nothing worth keeping; or perhaps the intermediate data or tools they use to accomplish a task is so valuable that it should be backed up, replicated, and widely broadcast to other agents. Existing distributed systems don't model statefulness, longevity, and fungibility as a spectrum like this.

---

## 4. Fully Semantic Computing

LLM agents are computers that can understand and extend themselves using semantics shared with human natural language. Some worry this means losing visibility into what computers are doing.

**I believe the opposite is true.**

### Agents Make Computing More Human-Legible
Let's reframe the problem: Computing through highly capable agents becomes, from the human perspective, **fully semantic**.

For an agent to "know thyself" and fulfill the semantics of a particular human request, there must be a stable mapping between the request's semantics, the agent's internal semantics, and the environment's semantics (APIs, data, tools). As agents accomplish tasks humans haven't fully automated, they will need to make their shared computing environment *more semantic* to aid discoverability.

So as agents solve previously-unautomated problems, they:

* Search for functionality matching human semantic requests
* Create new capabilities when none exist
* Make these discoverable for future agents
* Use names and structures that match human understanding for maximum discoverability

This mirrors how humans create domain-specific languages, product names, abstractions and tools, and professional jargon and vocabulary. But, agents can automate even this. The semantics stay universally legible because human-agent communication and agent-agent communication need to maintain shared semantics, just like human-human communication does already.

Every namespace, type, or service given a discoverable, legible name then results in computers **more closely** matching human semantics. That's why I think Collector is a way to make computing completely semantic and **more human**, with the help of our robot friends.

---

# Design

Contrary to the current implementation, I intend to implement Collector iteratively by building on top of very intentionally tiered abstractions:

1. Collection: proto/grpc->sqlite datastructure. Handling at the fs, query layers.

2. CollectionService: grpc service for Collections with CRUD, Search, other default APIs and the capacity for general grpc methods to be used on Collections' proto types via the Invoke API

3. CollectionServer: grpc **server** with a system or "bootstrap" CollectionService and a default interceptor that is allowed to query and call this service to implement authn/authz. Multiple CollectionServices could be registered to the same server, possibly mediated by the boostrap/system collection and the interceptor's specific logic (if any), but there can be only one bootstrap CollectionService. 

4. CollectionController: A CollectionServer where the bootstrap CollectionService and default interceptor handle namespace resolution via a namespace-interceptor that gets called before any authn/authz interceptors, which could be configured at the bootstrap (system) level and/or at the namespace level. The bootstrap collection consists of Controller CollectionServices and Namespace Collections that routing/implementing CollectionServer semantics within destination namespaces. These can be nested.

5. Collector: Controllers are collections of CollectionRepos (not just generic CollectionServices), which aggregates/handles Collections within a namespace, and operates default Collections like the CollectionRegistry for proto messages and grpc methods, and Logging/Operations Collections. This makes each CollectionServer fully reflective and "production ready" with tools for backups and searching across collections themselves.

6. Collector Network: Collector operates in nodes but there are root/hierarchical Collectors that serve as more authoritative sources of truth for namespaces and CollectionRegistry data, and also maintain Collector/Service registries. That allows Collector to discover information and coordinate with other Collectors across a network sharing a common root (and thus common registries and source of truth). I think this probably also requires the notion of a "Resource" to aggregate and group disparate Collector nodes.

7. Collector Mesh: Collectors should be able to connect and coordinate with each other even if they don't share a common network root/source of truth. I'm not sure this is actually a different milestone from the previous one, besides resolving types/information across roots and handling conflicts, which doesn't seem that complicated compared to everything else.

### Notes

The current implementation doesn't properly enforce these strictly separable, layered abstractions, so it's kind of a mess. We need to be more systematic and bottoms-up.

Also, I think most of the tests should be integration tests that live in a separate repo, so that LLMs are forced to implement/test/document a well-defined interface that is understandable and legible even without implementation details. **WORK IN PROGRESS**
