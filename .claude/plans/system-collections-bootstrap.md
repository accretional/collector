# System Collections Bootstrap Plan

## Vision
All system tracking (collections, types, connections, audit, logs) should be collections themselves, creating a self-describing, self-documenting system where the infrastructure uses its own abstractions.

## Bootstrap Sequence

```
1. Collection Registry (system/collections)
   ↓ registers itself as first member
2. Type Registry (system/types)
   ↓ registers all collector proto types including itself
3. Enable Type Validation
   ↓ all requests validated against type registry
4. Connection Collection (system/connections)
5. Audit Collection (system/audit)
6. Logs Collection (system/logs) ✓
```

---

## 1. Collection Registry as Collection

### Design

**Self-Referential Bootstrap:**
```go
// 1. Create collection registry with special bootstrap flag
registryCollection := NewCollection(
    &pb.Collection{
        Namespace: "system",
        Name: "collections",
        MessageType: &pb.MessageTypeRef{
            MessageName: "collector.Collection",
        },
    },
    store,
    fs,
    WithBootstrap(true), // Skip validation during bootstrap
)

// 2. Register itself as the first collection
registryCollection.Create(ctx, &pb.CollectionRecord{
    Id: "system/collections",
    ProtoData: marshal(&pb.Collection{
        Namespace: "system",
        Name: "collections",
        MessageType: &pb.MessageTypeRef{
            MessageName: "collector.Collection",
        },
        Metadata: &pb.Metadata{
            Labels: map[string]string{
                "system": "true",
                "bootstrap": "true",
                "self_referential": "true",
            },
        },
    }),
})

// 3. Now it's bootstrapped and can track other collections normally
```

**Current State:** ✅ **FULLY IMPLEMENTED**
- `SystemCollections.CollectionRegistry` in `pkg/bootstrap/system_collections.go`
- Self-referential bootstrap in `bootstrapCollectionRegistry()`
- Stores `pb.Collection` protos in `system/collections` collection
- Tests in `pkg/bootstrap/system_collections_test.go`

**Production Usage (server.go:227):**
- `CollectionRegistryStore` wraps `system/collections` Collection
- Implements `RegistryStore` interface
- `CollectionRepo` uses this in production
- Tests also use `CollectionRegistryStore` via `NewCollectionRegistryStoreFromStore()` helper

**Benefits (already available):**
- ✅ Search collections: `"indexed_fields contains email"`
- ✅ Query by labels: `"environment=production"`
- ✅ Full-text search collection metadata
- ✅ Collection registry itself gets backed up
- ✅ Audit trail of collection changes

---

## 2. Type Registry as Collection

### Design

**Purpose:** Track all proto types used in the system, validate requests against registered types.

**Collection:**
```proto
// system/types stores RegisteredType messages
message RegisteredType {
    string type_id = 1;           // e.g., "collector.Collection"
    string namespace = 2;          // e.g., "system"
    google.protobuf.FileDescriptorProto descriptor = 3;
    repeated string fields = 4;    // Field names for validation
    map<string, string> metadata = 5;
    google.protobuf.Timestamp registered_at = 6;
}
```

**Bootstrap:**
```go
// 1. Create type registry collection
typeRegistry := NewCollection(
    &pb.Collection{
        Namespace: "system",
        Name: "types",
        MessageType: &pb.MessageTypeRef{
            MessageName: "collector.RegisteredType",
        },
    },
    store, fs,
)

// 2. Register all collector proto types
types := []string{
    "collector.Collection",
    "collector.CollectionRecord",
    "collector.RegisteredType",
    "collector.Connection",
    "collector.AuditEvent",
    "collector.BackupMetadata",
    // ... all collector types
}

for _, typeName := range types {
    descriptor := getFileDescriptor(typeName)
    typeRegistry.Create(ctx, &CollectionRecord{
        Id: typeName,
        ProtoData: marshal(&pb.RegisteredType{
            TypeId: typeName,
            Namespace: "system",
            Descriptor: descriptor,
            Fields: extractFields(descriptor),
            RegisteredAt: timestamppb.Now(),
        }),
    })
}

// 3. Enable validation interceptor
server := NewServerWithTypeValidation(typeRegistry)
```

**Type Validation Interceptor:**
```go
func TypeValidationInterceptor(typeRegistry *Collection) grpc.UnaryServerInterceptor {
    return func(ctx, req, info, handler) {
        // Extract message type from request
        msgType := getMessageType(req)

        // Validate against type registry
        registered, err := typeRegistry.Get(ctx, msgType)
        if err != nil {
            return nil, status.Errorf(codes.FailedPrecondition,
                "type %s not registered", msgType)
        }

        // Validate fields exist
        if err := validateFields(req, registered); err != nil {
            return nil, status.Errorf(codes.InvalidArgument,
                "field validation failed: %v", err)
        }

        return handler(ctx, req)
    }
}
```

**Current State:**
- Registry has `registered_protos` and `registered_services` collections
- These are separate - should be unified into type registry

**Migration:**
- Merge `registered_protos` and `registered_services` into `system/types`
- Add validation interceptor
- All requests validated against type registry

---

## 3. Connection Collection

### Design

**Purpose:** Track all connections between collectors with full history.

**Collection:**
```proto
// system/connections stores Connection messages
message Connection {
    string id = 1;                  // conn_addr_timestamp
    string source_collector_id = 2;
    string target_collector_id = 3;
    string address = 4;
    repeated string shared_namespaces = 5;

    ConnectionState state = 6;      // CONNECTING, ACTIVE, CLOSED, ERROR

    Metadata metadata = 7;          // Created/updated times, labels
    google.protobuf.Timestamp last_activity = 8;

    // Stats
    int64 requests_served = 9;
    int64 bytes_transferred = 10;
    google.protobuf.Timestamp closed_at = 11;
    string close_reason = 12;
}

enum ConnectionState {
    CONNECTING = 0;
    ACTIVE = 1;
    CLOSED = 2;
    ERROR = 3;
}
```

**Implementation:**
```go
type ConnectionRegistry struct {
    collection *Collection  // system/connections
    active     map[string]*ConnectionState  // In-memory cache
    mu         sync.RWMutex
}

func (r *ConnectionRegistry) HandleConnect(ctx, req) (*ConnectResponse, error) {
    // Create connection record
    conn := &pb.Connection{
        Id: generateConnectionID(req.Address),
        SourceCollectorId: req.CollectorId,
        TargetCollectorId: r.collectorID,
        Address: req.Address,
        SharedNamespaces: findSharedNamespaces(req.Namespaces),
        State: pb.ConnectionState_ACTIVE,
        Metadata: &pb.Metadata{
            CreatedAt: timestamppb.Now(),
            UpdatedAt: timestamppb.Now(),
            Labels: req.Metadata,
        },
        LastActivity: timestamppb.Now(),
    }

    // Persist to collection
    if err := r.collection.Create(ctx, toRecord(conn)); err != nil {
        return nil, err
    }

    // Cache in memory for fast access
    r.active[conn.Id] = &ConnectionState{Connection: conn}

    return &pb.ConnectResponse{ConnectionId: conn.Id, ...}, nil
}

func (r *ConnectionRegistry) UpdateActivity(ctx, connID) {
    conn := r.active[connID]
    conn.LastActivity = time.Now()
    conn.RequestsServed++

    // Periodically flush to collection (not every request)
    if conn.RequestsServed % 100 == 0 {
        r.collection.Update(ctx, toRecord(conn.Connection))
    }
}

func (r *ConnectionRegistry) CloseConnection(ctx, connID, reason) {
    conn := r.active[connID]
    conn.State = pb.ConnectionState_CLOSED
    conn.ClosedAt = timestamppb.Now()
    conn.CloseReason = reason

    // Final update to collection
    r.collection.Update(ctx, toRecord(conn.Connection))

    // Remove from active cache
    delete(r.active, connID)
}
```

**Queries Enabled:**
```go
// Find active connections
connections := r.collection.Search(ctx, &SearchQuery{
    Filters: map[string]Filter{
        "state": {Operator: OpEquals, Value: "ACTIVE"},
    },
})

// Connection history for collector
connections := r.collection.Search(ctx, &SearchQuery{
    Filters: map[string]Filter{
        "source_collector_id": {Operator: OpEquals, Value: "collector-001"},
    },
    OrderBy: "last_activity",
    Ascending: false,
})

// Connections closed with errors in last 24h
connections := r.collection.Search(ctx, &SearchQuery{
    Filters: map[string]Filter{
        "state": {Operator: OpEquals, Value: "ERROR"},
        "closed_at": {Operator: OpGT, Value: time.Now().Add(-24*time.Hour).Unix()},
    },
})
```

**Current State:** ✅ **IMPLEMENTED**
- `ConnectionManager` in `pkg/dispatch/connection_manager.go`
- Persists connections to collection (optional, nil-safe)
- `ActiveConnection` struct for in-memory state (gRPC clients, activity)
- `Connection` proto updated with lifecycle timestamps, session tracking, usage stats
- `RecoverFromRestart()` marks stale connections from previous sessions
- Tests in `pkg/dispatch/connection_persistence_test.go`

**Implementation Details:**
- Session IDs track collector restarts (format: `{collectorID}_{timestamp_nano}`)
- Connection statuses: ACTIVE, DISCONNECTED, FAILED, STALE
- Stats tracked: request_count, bytes_sent, bytes_received, reconnect_count
- In-memory cache maintained for performance with fallback when no persistence

---

## 4. Audit Collection

### Design

**Purpose:** Track all operations for compliance, debugging, security.

**Collection:**
```proto
// system/audit stores AuditEvent messages
message AuditEvent {
    string id = 1;                      // event_timestamp_random
    string event_type = 2;              // CREATE_COLLECTION, DELETE_RECORD, etc.
    string actor = 3;                   // User/service that performed action
    string resource = 4;                // What was acted upon (ns/collection/id)
    string action = 5;                  // create, update, delete, search, backup

    google.protobuf.Timestamp timestamp = 6;

    // Request/response details
    google.protobuf.Struct request = 7;
    google.protobuf.Struct response = 8;

    // Context
    string collector_id = 9;
    string connection_id = 10;
    string namespace = 11;

    // Outcome
    bool success = 12;
    string error_message = 13;

    // Metadata
    map<string, string> labels = 14;
    string source_ip = 15;
}
```

**Implementation:**
```go
type AuditLogger struct {
    collection *Collection  // system/audit
    buffer     []*pb.AuditEvent
    mu         sync.Mutex
}

func (a *AuditLogger) Log(ctx, event *pb.AuditEvent) {
    // Buffer events for batch insert
    a.mu.Lock()
    a.buffer = append(a.buffer, event)
    shouldFlush := len(a.buffer) >= 100
    a.mu.Unlock()

    if shouldFlush {
        a.Flush(ctx)
    }
}

func (a *AuditLogger) Flush(ctx) {
    a.mu.Lock()
    events := a.buffer
    a.buffer = nil
    a.mu.Unlock()

    // Batch insert to collection
    for _, event := range events {
        a.collection.Create(ctx, toRecord(event))
    }
}

// Audit interceptor
func AuditInterceptor(auditor *AuditLogger) grpc.UnaryServerInterceptor {
    return func(ctx, req, info, handler) {
        start := time.Now()

        // Call handler
        resp, err := handler(ctx, req)

        // Log audit event
        event := &pb.AuditEvent{
            Id: generateEventID(),
            EventType: extractEventType(info.FullMethod),
            Actor: extractActor(ctx),
            Resource: extractResource(req),
            Action: extractAction(info.FullMethod),
            Timestamp: timestamppb.Now(),
            Request: toStruct(req),
            Response: toStruct(resp),
            Success: err == nil,
            ErrorMessage: getErrorMsg(err),
        }

        auditor.Log(ctx, event)

        return resp, err
    }
}
```

**Queries Enabled:**
```go
// All operations by user in last hour
events := auditor.collection.Search(ctx, &SearchQuery{
    Filters: map[string]Filter{
        "actor": {Operator: OpEquals, Value: "user@example.com"},
        "timestamp": {Operator: OpGT, Value: time.Now().Add(-1*time.Hour).Unix()},
    },
})

// Failed operations
events := auditor.collection.Search(ctx, &SearchQuery{
    Filters: map[string]Filter{
        "success": {Operator: OpEquals, Value: false},
    },
})

// Deletions in production namespace
events := auditor.collection.Search(ctx, &SearchQuery{
    Filters: map[string]Filter{
        "namespace": {Operator: OpEquals, Value: "production"},
        "action": {Operator: OpEquals, Value: "delete"},
    },
})
```

---

## 5. Logs Collection ✅ **IMPLEMENTED**

### Design

**Purpose:** Structured logging stored as collection for querying.

**Current State:** ✅ **FULLY IMPLEMENTED**
- `SystemLogger` in `pkg/collection/system_logger.go`
- Implements `logging.Logger` interface (Debug, Info, Warn, Error, With)
- Buffered async writes via worker goroutine (1000 entry buffer)
- Used by server.go for all structured logging
- Stores `SystemLog` proto messages in `system/logs` collection

**Implementation Details:**
```go
// Create logger from system logs collection
sysLogger := collection.NewSystemLogger(systemCollections.Logs)
s.log = sysLogger.With("collector_id", config.CollectorID)

// Usage throughout server
s.log.Info("Server started", "address", addr)
s.log.Warn("Operation failed", "error", err)
```

**Proto (SystemLog):**
- `id`: UUID
- `timestamp`: When logged
- `level`: DEBUG, INFO, WARNING, ERROR
- `message`: Log message
- `component`: Source component
- `fields`: Structured key-value pairs

**Query Examples:**
```go
// Find errors from last hour
logs := collection.Search(ctx, &SearchQuery{
    LabelFilters: map[string]string{"level": "ERROR"},
})

// Full-text search on messages
logs := collection.Search(ctx, &SearchQuery{
    TextQuery: "connection refused",
})

---

## Implementation Order

### Phase 1: Collection Registry ✅ **COMPLETE**
1. ✅ `system/collections` collection exists (`pkg/bootstrap/system_collections.go`)
2. ✅ Self-referential bootstrap implemented in `bootstrapCollectionRegistry()`
3. ✅ `CollectionRegistryStore` wraps collection, implements `RegistryStore` interface
4. ✅ Server uses `CollectionRegistryStore` in production (server.go:227)
5. ✅ Search via Collection API available
6. ✅ Bootstrap sequence tested in `system_collections_test.go`
7. ✅ Tests use same `CollectionRegistryStore` as production (via `NewCollectionRegistryStoreFromStore()`)

### Phase 2: Type Registry (Partial - registry collections exist)
1. ✅ `system/registered_protos` and `system/registered_services` collections exist
2. ✅ Collections now set MessageType field
3. ⬜ Define unified `RegisteredType` proto
4. ⬜ Implement type validation interceptor
5. ⬜ Merge protos/services into unified type registry

### Phase 3: Connection Collection ✅ **COMPLETE**
1. ✅ Enhanced `Connection` proto with state, timestamps, session tracking
2. ✅ `ConnectionManager` persists to collection (optional)
3. ✅ In-memory `ActiveConnection` cache for performance
4. ✅ `RecoverFromRestart()` marks stale connections
5. ✅ Activity tracking (request_count, bytes_sent/received)
6. ✅ Tests in `connection_persistence_test.go`

### Phase 4: Audit Collection (Partial - interceptor exists)
1. ✅ `AuditEvent` proto exists
2. ✅ Basic audit interceptor in `pkg/collection/audit.go`
3. ⬜ Buffered audit logger for batch inserts
4. ⬜ Test audit queries

### Phase 5: Logs Collection ✅ **COMPLETE**
1. ✅ `SystemLogger` implements `logging.Logger` interface
2. ✅ `system/logs` collection created with `SystemLog` proto
3. ✅ Buffered async worker with 1000 entry channel
4. ✅ Used throughout server.go for structured logging
5. ⬜ Add retention/cleanup (future enhancement)

---

## Benefits

### Immediate
- ✅ Unified architecture - everything is a collection
- ✅ Rich querying - search/filter all system data
- ✅ Self-documenting - system describes itself
- ✅ Backup/restore - system data backed up automatically

### Long-term
- ✅ Observability - query connections, audit, logs
- ✅ Compliance - full audit trail
- ✅ Debugging - trace operations through audit/logs
- ✅ Analytics - analyze system usage patterns
- ✅ Monitoring - query for anomalies

### Dogfooding
- ✅ Validates Collection abstraction
- ✅ Ensures Collection is robust enough for all use cases
- ✅ Tests features under real load (system operations)

---

## Files to Create/Modify

**New:**
- `pkg/collection/system_collections.go` - Bootstrap logic
- `pkg/collection/type_registry.go` - Type validation
- `pkg/collection/connection_registry.go` - Connection collection
- `pkg/collection/audit_logger.go` - Audit collection
- `pkg/collection/collection_logger.go` - Logs stub
- `proto/system.proto` - System collection messages

**Modified:**
- `pkg/collection/registry_store.go` - Migrate to collection
- `pkg/dispatch/connection_manager.go` - Use connection collection
- `cmd/server/main.go` - Bootstrap sequence
- `pkg/registry/registry.go` - Merge into type registry

---

## Testing Strategy

**Unit Tests:**
- Test bootstrap sequence
- Test self-referential collection
- Test type validation
- Test audit logging

**Integration Tests:**
- Test full bootstrap from scratch
- Test collection registry queries
- Test connection history
- Test audit trail

**Load Tests:**
- Benchmark audit logging overhead
- Test connection collection at scale
- Validate in-memory cache performance

---

## Notes

**Critical Design Decisions:**

1. **Self-Reference**: Collection registry references itself - must handle bootstrap carefully
2. **Performance**: Connection/audit need buffering - can't write on every operation
3. **Type Safety**: Type registry enables runtime validation - consider performance impact
4. **Unified Implementation**: Tests and production both use `CollectionRegistryStore` for consistency
5. **Logs Volume**: Logs collection could be huge - need retention policy from day 1

**Success Criteria:**

- [x] System boots with all system collections (bootstrap package)
- [x] Can query collection registry (system/collections via CollectionRegistryStore)
- [ ] Type validation works on all requests
- [x] Connection history is queryable (implemented with persistence)
- [x] Connection crash recovery works (RecoverFromRestart)
- [x] Audit trail captures operations (basic interceptor exists)
- [x] All tests pass
- [ ] Performance is acceptable (<10ms overhead)
