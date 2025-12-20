# Race Condition Analysis

## Overview

This document analyzes potential race conditions in the Collector system and proposes solutions using a system/operations collection for distributed coordination.

## Current Locking Architecture

### Per-Component Locks (Local)

1. **SqliteStore** (`pkg/db/sqlite/store.go`)
   - `mu sync.RWMutex` - protects individual store operations
   - Read operations: RLock
   - Write operations: Lock

2. **CollectionService** (`pkg/collection/service.go`)
   - `mu sync.RWMutex` - protects collection map
   - GetCollection: RLock
   - CreateCollection: Lock

3. **SqliteRegistryStore** (`pkg/collection/registry_store.go`)
   - `mu sync.RWMutex` - protects registry database
   - Read operations: RLock
   - Write operations: Lock

4. **BackupManager** (`pkg/collection/backup.go`)
   - `mu sync.Mutex` - serializes all backup operations
   - ALL operations: Lock (no concurrent backups)

5. **BackupMetadataStore** (`pkg/collection/backup.go`)
   - `mu sync.RWMutex` - protects metadata database
   - Read operations: RLock
   - Write operations: Lock

6. **Dispatcher** (`pkg/dispatch/dispatcher.go`)
   - `servicesMutex sync.RWMutex` - protects service routing
   - Route lookup: RLock
   - Service registration: Lock

7. **ConnectionManager** (`pkg/dispatch/connection_manager.go`)
   - `connectionsMutex sync.RWMutex` - protects connection map
   - `clientsMutex sync.RWMutex` - protects client cache

## Identified Race Conditions

### 1. 🔴 CRITICAL: Backup Timestamp Collisions

**Problem:**
```go
// pkg/collection/backup.go:364
timestamp := time.Now().Unix()  // Second precision!
backupPath := .../{name}-{timestamp}.db
```

**Race:**
- Two backups of same collection within 1 second → same filename
- Second backup overwrites first backup file
- Both backup metadata entries created (orphan references)

**Impact:** Data loss, corrupted backups

**Solution:** Use microsecond timestamps or UUID in filename

---

### 2. 🔴 CRITICAL: Backup During Delete

**Problem:**
```go
// Thread 1: DeleteCollection
repo.DeleteCollection(ns, name)  // Deletes DB file

// Thread 2: BackupCollection
backup.BackupCollection(ns, name)  // Tries to backup deleted collection
```

**Race:**
- Collection deleted while backup in progress
- Backup reads partially deleted data
- Or backup fails with "file not found"

**Impact:** Failed backups, partial data

**Solution:** Operation locking via system/operations collection

---

### 3. 🟡 HIGH: Restore Overwrites Active Collection

**Problem:**
```go
// Thread 1: Writing to collection
collection.CreateRecord(...)

// Thread 2: Restore backup
backup.RestoreBackup(...)  // Overwrites entire database
```

**Race:**
- Active writes happening during restore
- Restore completely replaces database file
- In-flight writes lost

**Impact:** Data loss

**Solution:** Require explicit overwrite=true, lock collection during restore

---

### 4. 🟡 HIGH: Concurrent Backups of Same Collection

**Problem:**
```go
// BackupManager uses full lock, BUT:
// If multiple BackupManagers exist (different processes?)
// Or if lock doesn't prevent timestamp collision
```

**Race:**
- Two backup requests at exactly the same second
- Same backup path generated
- File system race on creation

**Impact:** File corruption, backup failure

**Solution:** Atomic operation registration in system/operations

---

### 5. 🟡 HIGH: Cleanup During Backup

**Problem:**
```go
// pkg/collection/backup.go:507
go bm.cleanupOldBackups(...)  // Async, no coordination

// Meanwhile, another backup starts...
// Or a restore tries to use an old backup
```

**Race:**
- Cleanup deletes backup file
- Concurrent restore tries to read that backup
- Or concurrent ListBackups shows inconsistent state

**Impact:** Restore failures, file not found errors

**Solution:** Operation locking, cleanup checks for active operations

---

### 6. 🟠 MEDIUM: Collection Creation Race

**Problem:**
```go
// Thread 1 & 2: Both try to create same collection
repo.CreateCollection(ns, name, ...)
repo.CreateCollection(ns, name, ...)
```

**Race:**
- Both check if collection exists (not found)
- Both try to create database file
- Both try to insert into registry

**Current Mitigation:** Lock in CollectionService.CreateCollection
**Remaining Issue:** Not distributed - only works within single process

**Solution:** Atomic insert in registry with UNIQUE constraint

---

### 7. 🟠 MEDIUM: Registry Read/Write Race

**Problem:**
```go
// Thread 1: Reading collections
repos := registry.ListCollections()

// Thread 2: Creating new collection
registry.SaveCollection(newCollection)
```

**Race:**
- Read sees partial state during write
- Inconsistent view of collections

**Current Mitigation:** RWMutex in SqliteRegistryStore
**Limitation:** Only protects single process

**Solution:** Database-level locking (SQLite handles this)

---

### 8. 🟠 MEDIUM: Type Registry Race

**Problem:**
```go
// Thread 1: Creating collection with type X
typeRegistry.RegisterType(typeX)

// Thread 2: Creating collection with type X
typeRegistry.RegisterType(typeX)  // Already exists?
```

**Race:**
- Duplicate type registration
- Validation inconsistencies

**Current Mitigation:** Likely has checks, need to verify
**Solution:** Idempotent registration, system/types collection

---

### 9. 🟠 MEDIUM: System Collection Bootstrap Race

**Problem:**
```go
// Process 1 & 2: Both starting up
bootstrap.BootstrapSystemCollections()
bootstrap.BootstrapSystemCollections()
```

**Race:**
- Both try to create system/types
- Both try to create system/collections
- File system conflicts, registry conflicts

**Current Status:** Needs investigation
**Solution:** Idempotent bootstrap, file locking, or leader election

---

### 10. 🟢 LOW: BackupOnline Test Flakiness

**Problem:**
```go
// pkg/db/sqlite/store.go:457
destDB.ExecContext(ctx, sql)  // Create table in destDB

// pkg/db/sqlite/store.go:462
s.db.ExecContext(ctx, "INSERT INTO backup.table ...")  // Use attached DB
```

**Issue:**
- Table created in wrong database context
- ATTACH DATABASE sync issues

**Impact:** Test failures, not production issue
**Solution:** Fix table creation to use attached database

---

## Cross-Collection Races

### Independent Collections (Should Be Safe)

✅ Operations on `namespace1/collectionA` should not affect `namespace2/collectionB`
- Each has own database file
- Each has own Store instance with own lock
- Registry operations are serialized

### Shared Resources (Potential Issues)

🟡 **Registry Store** - All collections share single registry DB
- Protected by SqliteRegistryStore.mu
- Bottleneck under high collection creation rate

🟡 **Backup Metadata Store** - All backups share single metadata DB
- Protected by BackupMetadataStore.mu
- Potential contention

🟡 **System Collections** - Shared by all namespaces
- system/types
- system/collections
- system/connections
- Need same protection as user collections

---

## System vs Non-System Collection Races

### Current Separation

- System collections: `system/*` namespace
- User collections: Any other namespace
- Same underlying implementation (SqliteStore, Collection)

### Potential Issues

1. **Bootstrap vs Normal Operations**
   - Bootstrap creates system collections
   - Normal operations read from system collections
   - Race if bootstrap incomplete when operations start
   - **Solution:** Bootstrap before starting gRPC server (already done)

2. **System Collection Deletion**
   - What if user tries to delete system/types?
   - Could break entire system
   - **Solution:** Namespace protection, prevent deletion of system/*

3. **System Collection Backup**
   - Can/should users backup system collections?
   - If yes, same backup races apply
   - **Solution:** Allow but document carefully

---

## Proposed Solution: system/operations Collection

### Schema

```protobuf
message Operation {
  string operation_id = 1;        // UUID
  string type = 2;                // "backup", "restore", "clone", "delete", "cleanup"
  string namespace = 3;           // Target namespace
  string collection_name = 4;     // Target collection
  string status = 5;              // "pending", "in_progress", "completed", "failed"
  int64 started_at = 6;          // Unix timestamp
  int64 completed_at = 7;        // Unix timestamp
  string collector_id = 8;       // Which collector owns this
  map<string, string> metadata = 9; // Operation-specific data
  string error = 10;             // Error message if failed
}
```

### Usage Pattern

```go
// Before starting any long-running operation:
func (bm *BackupManager) BackupCollection(...) {
    // 1. Register operation
    op := RegisterOperation(ctx, "backup", namespace, name)
    defer CompleteOperation(ctx, op.ID)

    // 2. Check for conflicts
    conflicts := CheckConflicts(ctx, "backup", namespace, name)
    if len(conflicts) > 0 {
        return ErrOperationInProgress
    }

    // 3. Perform operation
    // ...
}
```

### Conflict Detection Rules

| Operation | Conflicts With |
|-----------|---------------|
| backup    | backup, restore, delete (same collection) |
| restore   | backup, restore, delete, any write (same collection) |
| delete    | backup, restore, clone (same collection) |
| cleanup   | restore (specific backup) |
| clone     | delete (same collection) |

### Benefits

✅ **Distributed coordination** - Works across multiple collectors
✅ **Operation history** - Audit trail of all operations
✅ **Conflict prevention** - Check before starting
✅ **Progress tracking** - Users can see active operations
✅ **Failure detection** - Identify stuck operations
✅ **Cleanup safety** - Check if backups are in use

---

## Implementation Priority

### Phase 1: Critical Fixes (Immediate)
1. ✅ Fix backup timestamp collisions (use microseconds or UUID)
2. ✅ Fix TestBackupOnline (ATTACH DATABASE bug)
3. ✅ Add system/operations collection to bootstrap
4. ✅ Implement operation registration in backup

### Phase 2: Safety Improvements (Short-term)
1. Add operation locking to restore
2. Add operation locking to delete
3. Prevent system collection deletion
4. Add cleanup coordination

### Phase 3: Enhanced Features (Medium-term)
1. Operation progress tracking
2. Operation timeout/failure detection
3. Distributed cleanup of stale operations
4. Performance monitoring

---

## Testing Strategy

### Unit Tests
- Test timestamp collision handling
- Test operation registration/conflict detection
- Test cleanup coordination

### Integration Tests
- Concurrent backup attempts
- Backup during delete
- Restore during active writes
- Cleanup during restore

### Stress Tests
- 100 concurrent backup requests
- High-frequency collection creation
- Rapid backup/restore cycles

---

## Open Questions

1. **Operation TTL**: How long to keep completed operations in system/operations?
2. **Failure Recovery**: What if collector crashes mid-operation?
3. **Lock Timeout**: Should operations timeout if they take too long?
4. **Distributed Cleanup**: How to clean up stale operations from dead collectors?
5. **Performance**: Will operation registration slow down critical paths?
