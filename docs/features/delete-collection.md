# DeleteCollection API

Permanently removes a collection including database, files, and metadata.

## Usage

```go
resp, err := client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "staging",
        Name:      "old-data",
    },
})

if err == nil && resp.Status.Code == pb.Status_OK {
    fmt.Printf("Deleted, freed %d bytes\n", resp.BytesFreed)
}
```

## Safety Features

- **Operation protection**: Cannot delete during active backup/restore/clone
- **Validation**: Checks namespace and collection name format
- **Atomic cleanup**: All-or-nothing deletion (DB + files + metadata)
- **Bytes reporting**: Returns total disk space freed

## Error Handling

| Error | Cause | Solution |
|-------|-------|----------|
| `operation in progress` | Backup/restore/clone running | Wait for completion |
| `collection not found` | Doesn't exist | Verify namespace/name |
| `invalid namespace` | Bad format | Use alphanumeric + hyphens |

## Best Practice

Always backup before deleting important collections:

```go
// Backup first
client.BackupCollection(ctx, &pb.BackupCollectionRequest{
    Collection: collection,
    Metadata: map[string]string{"reason": "pre-deletion"},
})

// Then delete
client.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
    Collection: collection,
})
```

## Implementation

- **Source**: `pkg/collection/repo.go:128-223`
- **Tests**: `pkg/collection/repo_test.go` (5 test cases)
