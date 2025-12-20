# Backup API

Comprehensive backup functionality for collections with automatic path management and retention policies.

## Overview

The Backup API provides point-in-time snapshots of collections **without creating collection metadata entries**. This is different from Clone, which creates a new active collection.

### Key Features

- ✅ **Auto-generated paths** - No manual path management
- ✅ **Retention policies** - Automatic cleanup based on age and count
- ✅ **Fixed backup directory** - Organized by namespace
- ✅ **Near-zero downtime** - 6-14ms lock duration during backup
- ✅ **Comprehensive metadata** - Track size, records, files, timestamps
- ✅ **Integrity verification** - SQLite PRAGMA checks

### Backup vs Clone

| Feature | Backup | Clone |
|---------|--------|-------|
| **Purpose** | Disaster recovery, archival | Create working copy |
| **Creates Collection Metadata** | ❌ No | ✅ Yes |
| **Shows in Discover()** | ❌ No | ✅ Yes |
| **Storage Location** | Fixed `.backup/` directory | Normal collection directory |
| **Path Management** | ✅ Auto-generated | ❌ Manual |
| **Metadata Tracking** | Separate backup database | Collection registry |
| **Retention Management** | ✅ Automatic cleanup | ❌ No |
| **Verification** | ✅ Integrity checks | ❌ No |
| **External Storage** | 🚧 Future (S3, GCS) | ❌ No |

## API Operations

### 1. BackupCollection

Creates a point-in-time snapshot of a collection with **automatically generated paths**.

**RPC:**
```protobuf
rpc BackupCollection(BackupCollectionRequest) returns (BackupCollectionResponse);
```

**Request:**
```protobuf
message BackupCollectionRequest {
  NamespacedName collection = 1;    // Collection to backup
  bool include_files = 3;            // Include filesystem data
  map<string, string> metadata = 4;  // Optional metadata (tags, notes)
}
```

**Response:**
```protobuf
message BackupCollectionResponse {
  Status status = 1;
  BackupMetadata backup = 2;      // Metadata about created backup
  int64 bytes_transferred = 3;
}
```

**Path Generation:**
Backups are automatically stored at:
```
{DataDir}/.backup/{namespace}/{collectionname}-{timestampseconds}.db
{DataDir}/.backup/{namespace}/{collectionname}-{timestampseconds}.files  (if include_files=true)
```

**Example:**
```go
resp, err := client.BackupCollection(ctx, &pb.BackupCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    IncludeFiles: true,
    Metadata: map[string]string{
        "type": "daily",
        "note": "Pre-migration backup",
    },
})

if resp.Status.Code == pb.Status_OK {
    fmt.Printf("Backup ID: %s\n", resp.Backup.BackupId)
    fmt.Printf("Storage Path: %s\n", resp.Backup.StoragePath)  // Auto-generated!
    fmt.Printf("Size: %d bytes\n", resp.BytesTransferred)
    fmt.Printf("Records: %d\n", resp.Backup.RecordCount)
}
// Example output:
// Backup ID: backup-a1b2c3d4e5f6g7h8
// Storage Path: /data/.backup/prod/users-1732233600.db
// Size: 1048576 bytes
// Records: 1000
```

### 2. ListBackups

Lists available backups with optional filtering.

**RPC:**
```protobuf
rpc ListBackups(ListBackupsRequest) returns (ListBackupsResponse);
```

**Request:**
```protobuf
message ListBackupsRequest {
  NamespacedName collection = 1;  // Optional: filter by collection
  string namespace = 2;           // Optional: all backups in namespace
  int32 limit = 3;                // Max backups to return
  int64 since_timestamp = 4;      // Only backups after this time
}
```

**Response:**
```protobuf
message ListBackupsResponse {
  Status status = 1;
  repeated BackupMetadata backups = 2;
  int64 total_count = 3;
}
```

**Examples:**
```go
// List all backups for a collection
resp, err := client.ListBackups(ctx, &pb.ListBackupsRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
})

for _, backup := range resp.Backups {
    fmt.Printf("Backup: %s (created: %v, size: %d)\n",
        backup.BackupId,
        time.Unix(backup.Timestamp, 0),
        backup.SizeBytes,
    )
}

// List all backups in a namespace
resp, err = client.ListBackups(ctx, &pb.ListBackupsRequest{
    Namespace: "prod",
    Limit:     10,
})

// List recent backups (last 7 days)
weekAgo := time.Now().AddDate(0, 0, -7).Unix()
resp, err = client.ListBackups(ctx, &pb.ListBackupsRequest{
    SinceTimestamp: weekAgo,
})
```

### 3. RestoreBackup

Restores a collection from a backup.

**RPC:**
```protobuf
rpc RestoreBackup(RestoreBackupRequest) returns (RestoreBackupResponse);
```

**Request:**
```protobuf
message RestoreBackupRequest {
  string backup_id = 1;           // Which backup to restore
  string dest_namespace = 2;      // Where to restore
  string dest_name = 3;           // Name of restored collection
  bool overwrite = 4;             // Allow overwriting existing collection
}
```

**Response:**
```protobuf
message RestoreBackupResponse {
  Status status = 1;
  string collection_id = 2;
  int64 records_restored = 3;
  int64 files_restored = 4;
}
```

**Example:**
```go
resp, err := client.RestoreBackup(ctx, &pb.RestoreBackupRequest{
    BackupId:      "backup-abc123",
    DestNamespace: "staging",
    DestName:      "users-restored",
    Overwrite:     false,
})

if resp.Status.Code == pb.Status_OK {
    fmt.Printf("Restored to: %s\n", resp.CollectionId)
    fmt.Printf("Records: %d\n", resp.RecordsRestored)
    fmt.Printf("Files: %d\n", resp.FilesRestored)
}
```

### 4. DeleteBackup

Deletes a backup and frees storage.

**RPC:**
```protobuf
rpc DeleteBackup(DeleteBackupRequest) returns (DeleteBackupResponse);
```

**Request:**
```protobuf
message DeleteBackupRequest {
  string backup_id = 1;
}
```

**Response:**
```protobuf
message DeleteBackupResponse {
  Status status = 1;
  int64 bytes_freed = 2;
}
```

**Example:**
```go
resp, err := client.DeleteBackup(ctx, &pb.DeleteBackupRequest{
    BackupId: "backup-abc123",
})

if resp.Status.Code == pb.Status_OK {
    fmt.Printf("Freed: %d bytes\n", resp.BytesFreed)
}
```

### 5. VerifyBackup

Verifies backup integrity (database corruption check).

**RPC:**
```protobuf
rpc VerifyBackup(VerifyBackupRequest) returns (VerifyBackupResponse);
```

**Request:**
```protobuf
message VerifyBackupRequest {
  string backup_id = 1;
}
```

**Response:**
```protobuf
message VerifyBackupResponse {
  Status status = 1;
  bool is_valid = 2;
  string error_message = 3;       // If invalid, what's wrong
  BackupMetadata backup = 4;
}
```

**Example:**
```go
resp, err := client.VerifyBackup(ctx, &pb.VerifyBackupRequest{
    BackupId: "backup-abc123",
})

if resp.IsValid {
    fmt.Println("Backup is valid")
} else {
    fmt.Printf("Backup invalid: %s\n", resp.ErrorMessage)
}
```

## Retention Policies

Collections can define **automatic retention policies** to manage backup lifecycle.

### BackupPolicy Configuration

```protobuf
message BackupPolicy {
  int32 max_backups = 1;          // Maximum number of backups to retain (0 = unlimited)
  int64 retention_seconds = 2;    // Delete backups older than this (0 = unlimited)
  bool enabled = 3;               // Enable automatic cleanup
}

message Collection {
  string namespace = 1;
  string name = 2;
  MessageTypeRef message_type = 3;
  repeated string indexed_fields = 4;
  string server_endpoint = 5;
  Metadata metadata = 6;
  BackupPolicy backup_policy = 7;  // Optional retention policy
}
```

### Setting Retention Policy

```go
// Create collection with retention policy
_, err := repoClient.CreateCollection(ctx, &pb.CreateCollectionRequest{
    Collection: &pb.Collection{
        Namespace:   "prod",
        Name:        "users",
        MessageType: &pb.MessageTypeRef{MessageName: "User"},
        BackupPolicy: &pb.BackupPolicy{
            MaxBackups:       7,              // Keep last 7 backups
            RetentionSeconds: 30 * 24 * 3600, // 30 days
            Enabled:          true,
        },
    },
})
```

### How Retention Works

**Automatic Cleanup Trigger:**
- Runs **asynchronously** after each successful backup
- Only if `BackupPolicy.Enabled = true`
- Non-blocking (goroutine) with error logging

**Retention Rules (applied in order):**
1. **Age-based retention**: Delete backups older than `retention_seconds`
2. **Count-based retention**: Keep only the `max_backups` newest backups

**Example Scenarios:**

```go
// Scenario 1: Keep only last 3 backups
BackupPolicy{
    MaxBackups: 3,
    Enabled:    true,
}
// Result: After 4th backup, oldest is deleted automatically

// Scenario 2: Keep backups for 7 days
BackupPolicy{
    RetentionSeconds: 7 * 24 * 3600,
    Enabled:          true,
}
// Result: Backups older than 7 days are deleted

// Scenario 3: Keep 10 backups AND max 30 days old
BackupPolicy{
    MaxBackups:       10,
    RetentionSeconds: 30 * 24 * 3600,
    Enabled:          true,
}
// Result: Deletes if age > 30 days OR keeping more than 10 backups
```

### Retention Safety Features

- ✅ **Opt-in**: Must explicitly set `Enabled = true`
- ✅ **Non-blocking**: Cleanup runs in background
- ✅ **Conservative**: Both conditions evaluated independently
- ✅ **Logged**: Cleanup actions logged for audit
- ✅ **Idempotent**: Safe to run multiple times

### Disabling Retention

```go
// Update collection to disable retention
err := collectionRepo.UpdateCollectionMetadata(ctx, "prod", "users", &pb.Collection{
    Namespace: "prod",
    Name:      "users",
    BackupPolicy: &pb.BackupPolicy{
        Enabled: false,  // Disable automatic cleanup
    },
})
```

## Backup Metadata

All backup operations track comprehensive metadata:

```protobuf
message BackupMetadata {
  string backup_id = 1;           // Unique backup identifier
  NamespacedName collection = 2;  // Source collection
  int64 timestamp = 3;            // Unix timestamp when created
  int64 size_bytes = 4;           // Total size
  int64 record_count = 5;         // Number of records
  int64 file_count = 6;           // Number of files
  bool includes_files = 7;        // Whether filesystem data included
  string storage_path = 8;        // Where backup is stored
  string storage_type = 9;        // "local", "s3", "gcs", etc.
  map<string, string> metadata = 10; // Custom metadata (tags, notes)
}
```

## Implementation Details

### Backup Storage Structure

```
./data/
├── backups/
│   └── metadata.db           # Backup metadata SQLite database
│
└── .backup/                  # Fixed backup directory
    ├── prod/                 # Namespace-based organization
    │   ├── users-1732233600.db
    │   ├── users-1732233600.files/
    │   ├── users-1732320000.db
    │   ├── orders-1732233600.db
    │   └── ...
    │
    └── staging/
        ├── test-data-1732233600.db
        └── ...
```

**Path Pattern:**
- Database: `{DataDir}/.backup/{namespace}/{name}-{timestamp}.db`
- Files: `{DataDir}/.backup/{namespace}/{name}-{timestamp}.files/`
- Metadata: `{DataDir}/backups/metadata.db`

### Metadata Database Schema

```sql
CREATE TABLE backups (
    backup_id TEXT PRIMARY KEY,
    collection_namespace TEXT NOT NULL,
    collection_name TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    size_bytes INTEGER NOT NULL,
    record_count INTEGER NOT NULL,
    file_count INTEGER NOT NULL,
    includes_files INTEGER NOT NULL,
    storage_path TEXT NOT NULL,
    storage_type TEXT NOT NULL,
    metadata TEXT,
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_collection ON backups(collection_namespace, collection_name);
CREATE INDEX idx_timestamp ON backups(timestamp);
```

### Backup Process

**Database Backup:**
1. Acquire read lock (allows concurrent operations)
2. Execute PASSIVE WAL checkpoint (non-blocking)
3. Use `VACUUM INTO` for consistent snapshot
4. Count records from source
5. Get database size
6. Release lock (total lock time: 6-14ms)

**Availability During Backup:**
- ✅ **Reads**: Fully available (402+ concurrent reads, 0 errors)
- ✅ **Writes**: Fully available (24+ concurrent writes, 0 errors)
- ✅ **Lock Duration**: 6-14ms (proven with tests)
- ✅ **Downtime**: Near-zero

(See [BACKUP_AVAILABILITY_VERIFIED.md](BACKUP_AVAILABILITY_VERIFIED.md) for proof)

**Filesystem Backup (if include_files=true):**
1. Create backup filesystem directory
2. Copy all files from source to backup location
3. Track bytes transferred
4. Count files

**Metadata Creation:**
1. Generate backup ID (hash of collection + timestamp)
2. Create BackupMetadata entry
3. Save to metadata database

### Backup ID Generation

```go
func generateBackupID(namespace, name string, timestamp int64) string {
    data := fmt.Sprintf("%s/%s@%d", namespace, name, timestamp)
    hash := sha256.Sum256([]byte(data))
    return fmt.Sprintf("backup-%s", hex.EncodeToString(hash[:])[:16])
}
```

Example: `backup-a1b2c3d4e5f6g7h8`

### Restore Process

1. Validate backup exists and is accessible
2. Verify backup integrity (optional)
3. Check destination doesn't exist (unless overwrite=true)
4. Copy backup database to collection directory
5. Copy backup files (if included)
6. Create collection metadata entry with restore labels:
   - `restored_from_backup`: backup ID
   - `original_collection`: original namespace/name
   - `backup_timestamp`: when backup was created

## Use Cases

### 1. Daily Automated Backups with Retention

```go
// Step 1: Create collection with retention policy (one-time setup)
_, err := repoClient.CreateCollection(ctx, &pb.CreateCollectionRequest{
    Collection: &pb.Collection{
        Namespace:   "prod",
        Name:        "users",
        MessageType: &pb.MessageTypeRef{MessageName: "User"},
        BackupPolicy: &pb.BackupPolicy{
            MaxBackups:       7,              // Keep last 7 backups
            RetentionSeconds: 30 * 24 * 3600, // 30 days
            Enabled:          true,           // Enable automatic cleanup
        },
    },
})

// Step 2: Scheduled backup (cron job or task)
func dailyBackup() {
    _, err := client.BackupCollection(ctx, &pb.BackupCollectionRequest{
        Collection: &pb.NamespacedName{
            Namespace: "prod",
            Name:      "users",
        },
        IncludeFiles: true,
        Metadata: map[string]string{
            "type":     "automated",
            "schedule": "daily",
        },
    })
    // Old backups are automatically cleaned up based on policy!
}
```

### 2. Pre-Migration Backup (Manual, No Retention)

```go
// For critical operations, create collection without retention policy
_, err := client.BackupCollection(ctx, &pb.BackupCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    IncludeFiles: true,
    Metadata: map[string]string{
        "type": "manual",
        "note": "Before schema migration v2.0",
    },
})
// Backup is kept indefinitely (no retention policy configured)
```

### 3. Manual Retention Management (Optional)

```go
// If you prefer manual cleanup instead of automatic retention:
func cleanupOldBackups() {
    thirtyDaysAgo := time.Now().AddDate(0, 0, -30).Unix()

    resp, _ := client.ListBackups(ctx, &pb.ListBackupsRequest{
        Namespace: "prod",
    })

    for _, backup := range resp.Backups {
        // Check custom metadata
        if retention, ok := backup.Metadata["permanent"]; ok && retention == "true" {
            continue
        }

        // Delete if old
        if backup.Timestamp < thirtyDaysAgo {
            client.DeleteBackup(ctx, &pb.DeleteBackupRequest{
                BackupId: backup.BackupId,
            })
        }
    }
}
```

### 4. Disaster Recovery

```go
// List available backups
listResp, _ := client.ListBackups(ctx, &pb.ListBackupsRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
})

// Find most recent valid backup
var latestBackup *pb.BackupMetadata
for _, backup := range listResp.Backups {
    verifyResp, _ := client.VerifyBackup(ctx, &pb.VerifyBackupRequest{
        BackupId: backup.BackupId,
    })

    if verifyResp.IsValid {
        latestBackup = backup
        break // Backups are sorted by timestamp DESC
    }
}

// Restore
if latestBackup != nil {
    _, err := client.RestoreBackup(ctx, &pb.RestoreBackupRequest{
        BackupId:      latestBackup.BackupId,
        DestNamespace: "prod",
        DestName:      "users",
        Overwrite:     true,
    })
}
```

## Testing

Comprehensive test coverage in `pkg/collection/backup_test.go`:

**Core Functionality:**
- ✅ **TestBackupCollection_Simple**: Basic backup functionality
- ✅ **TestListBackups**: Listing with filters
- ✅ **TestDeleteBackup**: Backup deletion
- ✅ **TestVerifyBackup**: Integrity verification
- ✅ **TestVerifyBackup_Missing**: Missing file detection
- ✅ **TestBackupValidation**: Request validation
- ✅ **TestBackupWithFiles**: Filesystem backup
- ✅ **TestBackupConcurrent**: Concurrent backup operations
- ✅ **TestBackupLargeDataset**: Large collection backup (10k records)
- ✅ **TestBackupEmptyCollection**: Empty collection handling
- ✅ **TestBackupWithSpecialCharacters**: Unicode and special chars
- ✅ **TestBackupMetadataFiltering**: Metadata filtering
- ✅ **TestRestoreWithOverwrite**: Restore with overwrite

**Retention Policy Tests:**
- ✅ **TestRetentionPolicyMaxBackups**: Count-based retention (keeps newest N)
- ✅ **TestRetentionPolicyAge**: Time-based retention (deletes older than X)

**Run tests:**
```bash
# All backup tests
go test ./pkg/collection -run "Test.*Backup" -v

# Retention policy tests only
go test ./pkg/collection -run "TestRetentionPolicy" -v
```

## Performance

**Backup Speed:**
- Small (100 records): ~3-5ms
- Medium (1,000 records): ~10-20ms
- Large (10,000 records): ~50-100ms

**Concurrent Operations During Backup:**
- **Reads**: 400+ operations/second
- **Writes**: 25+ operations/second
- **Availability**: 100% (0 errors)

## Future Enhancements

### External Storage Support

```go
// S3 backup (future)
_, err := client.BackupCollection(ctx, &pb.BackupCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    DestPath: "s3://my-bucket/backups/users-2025-11-22.db",
    Metadata: map[string]string{
        "storage_class": "GLACIER",
    },
})

// GCS backup (future)
_, err = client.BackupCollection(ctx, &pb.BackupCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    DestPath: "gcs://my-bucket/backups/users-2025-11-22.db",
})
```

### Incremental Backups

```go
// Future: only backup changes since last backup
_, err := client.BackupCollection(ctx, &pb.BackupCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    DestPath: "/backups/users-incremental.db",
    Incremental: true,
    BasedOnBackup: "backup-abc123",
})
```

### Compression

```go
// Future: compressed backups
_, err := client.BackupCollection(ctx, &pb.BackupCollectionRequest{
    Collection: &pb.NamespacedName{
        Namespace: "prod",
        Name:      "users",
    },
    DestPath: "/backups/users-2025-11-22.db.zst",
    Compression: "zstd",
})
```

## Summary

✅ **Fully Implemented:**
- **Auto-generated paths** - No manual path management
- **Fixed backup directory** - `{DataDir}/.backup/{namespace}/`
- **Retention policies** - Automatic cleanup (age and count-based)
- **BackupCollection** - Point-in-time snapshots
- **ListBackups** - Filtering by collection/namespace/timestamp
- **RestoreBackup** - With overwrite support
- **DeleteBackup** - Storage cleanup
- **VerifyBackup** - Integrity checks (SQLite PRAGMA)
- **Backup metadata** - Comprehensive tracking in SQLite
- **Test coverage** - 15 tests including retention policy tests
- **Near-zero downtime** - 6-14ms lock duration (proven)

🚧 **Future Work:**
- External storage (S3, GCS, Azure)
- Incremental backups
- Compression (zstd)
- Encryption at rest
- Backup streaming for large backups
- Scheduled backups API
- Multi-destination backups

📋 **Ready for:**
- Production use (local backups)
- Disaster recovery scenarios
- Compliance requirements (retention)
- Automated backup workflows with cleanup
- Zero-touch backup management
