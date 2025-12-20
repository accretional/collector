# DeleteCollection API

Complete collection deletion with operation state protection and disk space reporting.

## Overview

DeleteCollection permanently removes a collection and all associated data:
- ✅ SQLite database file
- ✅ Files directory and all contents
- ✅ Collection metadata from registry

**Safety:** Cannot delete during active backup/restore/clone operations.

## API

### RPC Definition

```protobuf
rpc DeleteCollection(DeleteCollectionRequest) returns (DeleteCollectionResponse);
```

### Request

```protobuf
message DeleteCollectionRequest {
  NamespacedName collection = 1;  // Collection to delete
}
```

**Fields:**
- `collection.namespace` - Namespace of the collection
- `collection.name` - Name of the collection

### Response

```protobuf
message DeleteCollectionResponse {
  Status status = 1;
  int64 bytes_freed = 2;  // Total bytes freed (DB + files)
}
```

**Fields:**
- `status` - Operation status (OK or error)
- `bytes_freed` - Total disk space freed in bytes

## Usage Examples

### Basic Deletion

```go
resp, err := client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "staging",
        Name:      "old-data",
    },
})

if err != nil {
    log.Fatalf("Delete failed: %v", err)
}

if resp.Status.Code == pb.Status_OK {
    fmt.Printf("✓ Deleted collection\n")
    fmt.Printf("  Freed: %d bytes (%.2f MB)\n",
        resp.BytesFreed,
        float64(resp.BytesFreed)/1024/1024)
}
```

### With Error Handling

```go
resp, err := client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
})

if err != nil {
    // Check if deletion was blocked by active operation
    if strings.Contains(err.Error(), "operation in progress") {
        fmt.Println("Cannot delete: backup/restore/clone in progress")
        fmt.Println("Wait for operation to complete and try again")
        return
    }
    log.Fatalf("Unexpected error: %v", err)
}

fmt.Printf("Deleted: %s/%s (%d bytes freed)\n",
    "prod", "users", resp.BytesFreed)
```

### Bulk Deletion

```go
// Delete multiple old test collections
testCollections := []string{"test-data-1", "test-data-2", "test-data-3"}
var totalFreed int64

for _, name := range testCollections {
    resp, err := client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
        Collection: &pb.NamespacedName{
            Namespace: "test",
            Name:      name,
        },
    })

    if err != nil {
        fmt.Printf("Failed to delete %s: %v\n", name, err)
        continue
    }

    totalFreed += resp.BytesFreed
    fmt.Printf("✓ Deleted %s\n", name)
}

fmt.Printf("\nTotal freed: %.2f MB\n", float64(totalFreed)/1024/1024)
```

## Operation State Protection

DeleteCollection participates in the operation state system to prevent race conditions.

### How It Works

**Before deletion:**
1. Retrieves collection metadata
2. Checks for active operations (backup, restore, clone)
3. Registers delete operation atomically
4. Prevents concurrent operations on the collection

**During deletion:**
1. Calculates bytes to be freed (database + files)
2. Closes collection database connection
3. Deletes database file
4. Recursively deletes files directory
5. Removes collection from registry

**After deletion:**
6. Clears operation state (gracefully handles already-deleted collection)

### Operation Conflicts

DeleteCollection will fail if any of these operations are active:

| Active Operation | Why Blocked |
|-----------------|-------------|
| Backup | Don't delete while creating backup |
| Restore | Don't delete during restore to this collection |
| Clone (as source) | Don't delete while being cloned |
| Clone (as dest) | Don't delete while receiving clone |
| Another Delete | Prevent duplicate deletion attempts |

**Timeout:** 31 seconds (30s + 1s padding)

If an operation times out or crashes, it's automatically cleaned up on server restart.

### Example: Blocked Deletion

```go
// Thread 1: Backup running
client.BackupCollection(ctx, &pb.BackupCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
})

// Thread 2: Try to delete (will fail)
resp, err := client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
})
// Error: "cannot delete: operation in progress (backup)"
```

## Safety Features

### 1. Validation

✅ **Namespace validation** - Ensures valid namespace format
✅ **Collection name validation** - Ensures valid collection name
✅ **Existence check** - Collection must exist to delete

```go
// Invalid namespace
resp, err := client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "invalid-namespace!",  // ❌ Special chars not allowed
        Name:      "data",
    },
})
// Error: "invalid namespace"
```

### 2. Conflict Detection

✅ **Active operations** - Checks for backup/restore/clone
✅ **Atomic registration** - Uses SQLite ACID transactions
✅ **Timeout protection** - Operations auto-expire after timeout

### 3. Atomic Cleanup

✅ **All-or-nothing** - Either everything deleted or nothing
✅ **Registry sync** - Metadata removed atomically
✅ **Error recovery** - Deferred operation state cleanup

### 4. Bytes Reporting

✅ **Accurate calculation** - Walks entire directory tree
✅ **Database size** - Includes main DB file
✅ **Files size** - Recursively calculates all files

## Implementation Details

### File Locations

```
{DataDir}/
├── collections/
│   └── {namespace}/
│       ├── {name}.db           # ← Deleted
│       └── {name}.files/       # ← Deleted (recursively)
│           ├── file1
│           ├── file2
│           └── ...
│
└── registry.db                 # ← Metadata removed
```

### Bytes Freed Calculation

```go
// Database file
dbPath := "{DataDir}/collections/{namespace}/{name}.db"
dbSize := os.Stat(dbPath).Size()

// Files directory (recursive)
filesPath := "{DataDir}/collections/{namespace}/{name}.files/"
filesSize := filepath.Walk(filesPath, func(path, info) {
    if !info.IsDir() {
        totalSize += info.Size()
    }
})

bytesFreed = dbSize + filesSize
```

### Operation URI Format

For debugging and monitoring, delete operations use this URI format:

```
delete:{namespace}/{name}
```

Example: `delete:prod/users`

This appears in operation state metadata and server logs.

### Source Code

**Implementation:** `pkg/collection/repo.go:128-223`

**Tests:** `pkg/collection/repo_test.go`
- TestCollectionRepo_DeleteCollection
- TestCollectionRepo_DeleteCollection_NonExistent
- TestCollectionRepo_DeleteCollection_InvalidRequest
- TestCollectionRepo_DeleteCollection_WithActiveOperation
- TestCollectionRepo_DeleteCollection_Multiple

## Use Cases

### 1. Cleanup Test Data

```go
// After integration tests, delete test collections
func cleanupTestData() {
    testNamespaces := []string{"test", "staging", "dev"}

    for _, ns := range testNamespaces {
        // List collections in namespace
        listResp, _ := client.Discover(ctx, &pb.DiscoverRequest{
            Namespace: ns,
        })

        for _, coll := range listResp.Collections {
            client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
                Collection: &pb.NamespacedName{
                    Namespace: coll.Namespace,
                    Name:      coll.Name,
                },
            })
        }
    }
}
```

### 2. Remove Obsolete Collections

```go
// Delete collections older than 30 days
func removeOldCollections(threshold time.Time) {
    listResp, _ := client.Discover(ctx, &pb.DiscoverRequest{})

    for _, coll := range listResp.Collections {
        createdAt := time.Unix(coll.Metadata.CreatedAt.Seconds, 0)

        if createdAt.Before(threshold) {
            fmt.Printf("Deleting old collection: %s/%s (created %v)\n",
                coll.Namespace, coll.Name, createdAt)

            client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
                Collection: &pb.NamespacedName{
                    Namespace: coll.Namespace,
                    Name:      coll.Name,
                },
            })
        }
    }
}
```

### 3. Space Management

```go
// Free up disk space by deleting large unused collections
func freeupSpace(targetBytes int64) {
    listResp, _ := client.Discover(ctx, &pb.DiscoverRequest{})

    var freed int64
    for _, coll := range listResp.Collections {
        if freed >= targetBytes {
            break
        }

        // Check if collection is unused (custom logic)
        if isUnused(coll) {
            resp, err := client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
                Collection: &pb.NamespacedName{
                    Namespace: coll.Namespace,
                    Name:      coll.Name,
                },
            })

            if err == nil {
                freed += resp.BytesFreed
                fmt.Printf("Freed %.2f MB\n", float64(resp.BytesFreed)/1024/1024)
            }
        }
    }

    fmt.Printf("Total freed: %.2f MB\n", float64(freed)/1024/1024)
}
```

## Error Handling

### Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `operation in progress` | Backup/restore/clone running | Wait for operation to complete |
| `collection not found` | Collection doesn't exist | Verify namespace and name |
| `invalid namespace` | Invalid namespace format | Use valid namespace (alphanumeric + hyphens) |
| `invalid collection name` | Invalid name format | Use valid name (alphanumeric + hyphens) |
| `failed to close collection` | Database connection issue | Check database health, retry |
| `failed to delete database file` | File system error | Check permissions, disk health |

### Retry Logic

```go
func deleteWithRetry(namespace, name string, maxRetries int) error {
    var lastErr error

    for i := 0; i < maxRetries; i++ {
        resp, err := client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
            Collection: &pb.NamespacedName{
                Namespace: namespace,
                Name:      name,
            },
        })

        if err == nil && resp.Status.Code == pb.Status_OK {
            return nil
        }

        lastErr = err

        // If operation in progress, wait and retry
        if strings.Contains(err.Error(), "operation in progress") {
            time.Sleep(time.Second * 5)
            continue
        }

        // Other errors are not retryable
        return err
    }

    return fmt.Errorf("max retries exceeded: %w", lastErr)
}
```

## Best Practices

### 1. Backup Before Delete

Always create a backup before deleting important collections:

```go
// Create backup
backupResp, err := client.BackupCollection(ctx, &pb.BackupCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    IncludeFiles: true,
    Metadata: map[string]string{
        "reason": "pre-deletion backup",
    },
})

if err != nil {
    log.Fatalf("Backup failed: %v", err)
}

fmt.Printf("Backup created: %s\n", backupResp.Backup.BackupId)

// Now safe to delete
client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
})
```

### 2. Verify Before Delete

Check collection contents before deletion:

```go
// List records to verify it's the right collection
listResp, _ := collectionClient.List(ctx, &pb.ListRequest{
    Namespace:      "test",
    CollectionName: "old-data",
    Limit:          10,
})

fmt.Printf("Collection contains %d records\n", len(listResp.Records))
fmt.Printf("First record: %v\n", listResp.Records[0])

// Confirm with user
fmt.Print("Delete this collection? (yes/no): ")
var confirm string
fmt.Scanln(&confirm)

if confirm == "yes" {
    client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
        Collection: &pb.NamespacedName{
            Namespace: "test",
            Name:      "old-data",
        },
    })
}
```

### 3. Monitor Disk Space

Track disk space before and after deletions:

```go
func monitoredDelete(namespace, name string) {
    // Check disk space before
    var statBefore syscall.Statfs_t
    syscall.Statfs(dataDir, &statBefore)
    freeBefore := statBefore.Bavail * uint64(statBefore.Bsize)

    // Delete collection
    resp, err := client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
        Collection: &pb.NamespacedName{
            Namespace: namespace,
            Name:      name,
        },
    })

    if err != nil {
        log.Fatalf("Delete failed: %v", err)
    }

    // Check disk space after
    var statAfter syscall.Statfs_t
    syscall.Statfs(dataDir, &statAfter)
    freeAfter := statAfter.Bavail * uint64(statAfter.Bsize)

    fmt.Printf("Reported freed: %.2f MB\n",
        float64(resp.BytesFreed)/1024/1024)
    fmt.Printf("Actual freed: %.2f MB\n",
        float64(freeAfter-freeBefore)/1024/1024)
}
```

## Summary

✅ **Fully Implemented:**
- Complete collection deletion (DB + files + metadata)
- Operation state protection
- Conflict detection
- Bytes freed reporting
- Comprehensive validation
- Atomic cleanup
- Error handling
- Test coverage (5 test cases)

🔒 **Safety Features:**
- Cannot delete during backup/restore/clone
- Validates namespace and collection name
- Atomic operation registration
- Graceful error recovery
- Timeout-based cleanup

📊 **Reporting:**
- Accurate bytes freed calculation
- Database size included
- Recursive files directory calculation
- Human-readable output

🎯 **Production Ready:**
- Tested and verified
- Race condition protection
- Comprehensive error handling
- Clear error messages
- Suitable for automated cleanup
