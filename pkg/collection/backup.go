package collection

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/fs/local"
	_ "modernc.org/sqlite"
)

// BackupManager manages backup operations for collections.
// Unlike Clone, backups create snapshots without registering them as active collections.
type BackupManager struct {
	repo      CollectionRepo
	transport Transport
	metaStore *BackupMetadataStore
	mu        sync.RWMutex
}

// BackupMetadataStore persists backup metadata to a SQLite database.
type BackupMetadataStore struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// NewBackupMetadataStore creates a new backup metadata store.
func NewBackupMetadataStore(dbPath string) (*BackupMetadataStore, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create metadata directory: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=10000", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata db: %w", err)
	}

	// Create schema
	schema := `
	CREATE TABLE IF NOT EXISTS backups (
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

	CREATE INDEX IF NOT EXISTS idx_collection ON backups(collection_namespace, collection_name);
	CREATE INDEX IF NOT EXISTS idx_timestamp ON backups(timestamp);
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return &BackupMetadataStore{db: db, path: dbPath}, nil
}

// Close closes the metadata store.
func (s *BackupMetadataStore) Close() error {
	return s.db.Close()
}

// SaveBackup saves backup metadata.
func (s *BackupMetadataStore) SaveBackup(ctx context.Context, backup *pb.BackupMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Serialize metadata map to JSON-like string
	var metaStr string
	if len(backup.Metadata) > 0 {
		parts := make([]string, 0, len(backup.Metadata))
		for k, v := range backup.Metadata {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		metaStr = strings.Join(parts, ";")
	}

	query := `
	INSERT INTO backups (
		backup_id, collection_namespace, collection_name, timestamp,
		size_bytes, record_count, file_count, includes_files,
		storage_path, storage_type, metadata, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		backup.BackupId,
		backup.Collection.Namespace,
		backup.Collection.Name,
		backup.Timestamp,
		backup.SizeBytes,
		backup.RecordCount,
		backup.FileCount,
		boolToInt(backup.IncludesFiles),
		backup.StoragePath,
		backup.StorageType,
		metaStr,
		time.Now().Unix(),
	)

	return err
}

// GetBackup retrieves backup metadata by ID.
func (s *BackupMetadataStore) GetBackup(ctx context.Context, backupID string) (*pb.BackupMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var (
		backup       pb.BackupMetadata
		namespace    string
		name         string
		includesFiles int
		metaStr      string
	)

	query := `
	SELECT backup_id, collection_namespace, collection_name, timestamp,
	       size_bytes, record_count, file_count, includes_files,
	       storage_path, storage_type, metadata
	FROM backups WHERE backup_id = ?
	`

	err := s.db.QueryRowContext(ctx, query, backupID).Scan(
		&backup.BackupId,
		&namespace,
		&name,
		&backup.Timestamp,
		&backup.SizeBytes,
		&backup.RecordCount,
		&backup.FileCount,
		&includesFiles,
		&backup.StoragePath,
		&backup.StorageType,
		&metaStr,
	)

	if err != nil {
		return nil, err
	}

	backup.Collection = &pb.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	backup.IncludesFiles = intToBool(includesFiles)

	// Parse metadata string
	if metaStr != "" {
		backup.Metadata = make(map[string]string)
		for _, part := range strings.Split(metaStr, ";") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				backup.Metadata[kv[0]] = kv[1]
			}
		}
	}

	return &backup, nil
}

// ListBackups lists backups with optional filters.
func (s *BackupMetadataStore) ListBackups(ctx context.Context, req *pb.ListBackupsRequest) ([]*pb.BackupMetadata, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build query with filters
	var whereClauses []string
	var args []interface{}

	if req.Collection != nil {
		whereClauses = append(whereClauses, "collection_namespace = ? AND collection_name = ?")
		args = append(args, req.Collection.Namespace, req.Collection.Name)
	} else if req.Namespace != "" {
		whereClauses = append(whereClauses, "collection_namespace = ?")
		args = append(args, req.Namespace)
	}

	if req.SinceTimestamp > 0 {
		whereClauses = append(whereClauses, "timestamp >= ?")
		args = append(args, req.SinceTimestamp)
	}

	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM backups %s", whereClause)
	var totalCount int64
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("failed to count backups: %w", err)
	}

	// Query backups
	query := fmt.Sprintf(`
	SELECT backup_id, collection_namespace, collection_name, timestamp,
	       size_bytes, record_count, file_count, includes_files,
	       storage_path, storage_type, metadata
	FROM backups %s
	ORDER BY timestamp DESC
	`, whereClause)

	if req.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", req.Limit)
		args = append(args, req.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query backups: %w", err)
	}
	defer rows.Close()

	var backups []*pb.BackupMetadata
	for rows.Next() {
		var (
			backup       pb.BackupMetadata
			namespace    string
			name         string
			includesFiles int
			metaStr      string
		)

		if err := rows.Scan(
			&backup.BackupId,
			&namespace,
			&name,
			&backup.Timestamp,
			&backup.SizeBytes,
			&backup.RecordCount,
			&backup.FileCount,
			&includesFiles,
			&backup.StoragePath,
			&backup.StorageType,
			&metaStr,
		); err != nil {
			return nil, 0, err
		}

		backup.Collection = &pb.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}
		backup.IncludesFiles = intToBool(includesFiles)

		// Parse metadata
		if metaStr != "" {
			backup.Metadata = make(map[string]string)
			for _, part := range strings.Split(metaStr, ";") {
				kv := strings.SplitN(part, "=", 2)
				if len(kv) == 2 {
					backup.Metadata[kv[0]] = kv[1]
				}
			}
		}

		backups = append(backups, &backup)
	}

	return backups, totalCount, nil
}

// DeleteBackup removes backup metadata.
func (s *BackupMetadataStore) DeleteBackup(ctx context.Context, backupID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, "DELETE FROM backups WHERE backup_id = ?", backupID)
	return err
}

// NewBackupManager creates a new backup manager.
func NewBackupManager(repo CollectionRepo, transport Transport, metaStorePath string) (*BackupManager, error) {
	metaStore, err := NewBackupMetadataStore(metaStorePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata store: %w", err)
	}

	return &BackupManager{
		repo:      repo,
		transport: transport,
		metaStore: metaStore,
	}, nil
}

// Close closes the backup manager.
func (bm *BackupManager) Close() error {
	return bm.metaStore.Close()
}

// BackupCollection creates a backup of a collection.
func (bm *BackupManager) BackupCollection(ctx context.Context, req *pb.BackupCollectionRequest) (*pb.BackupCollectionResponse, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Validate request
	if req.Collection == nil || req.Collection.Namespace == "" || req.Collection.Name == "" {
		return &pb.BackupCollectionResponse{
			Status: &pb.Status{
				Code:    pb.Status_INVALID_ARGUMENT,
				Message: "collection namespace and name are required",
			},
		}, nil
	}

	if req.DestPath == "" {
		return &pb.BackupCollectionResponse{
			Status: &pb.Status{
				Code:    pb.Status_INVALID_ARGUMENT,
				Message: "dest_path is required",
			},
		}, nil
	}

	// Get source collection
	sourceCollection, err := bm.repo.GetCollection(ctx, req.Collection.Namespace, req.Collection.Name)
	if err != nil {
		return &pb.BackupCollectionResponse{
			Status: &pb.Status{
				Code:    pb.Status_NOT_FOUND,
				Message: fmt.Sprintf("collection not found: %v", err),
			},
		}, nil
	}

	// Determine storage type from path
	storageType := "local"
	if strings.HasPrefix(req.DestPath, "s3://") {
		storageType = "s3"
	} else if strings.HasPrefix(req.DestPath, "gcs://") {
		storageType = "gcs"
	}

	// For now, only support local storage
	if storageType != "local" {
		return &pb.BackupCollectionResponse{
			Status: &pb.Status{
				Code:    pb.Status_UNIMPLEMENTED,
				Message: fmt.Sprintf("storage type %s not yet implemented", storageType),
			},
		}, nil
	}

	// Generate backup ID (hash of collection + timestamp)
	timestamp := time.Now().Unix()
	backupID := generateBackupID(req.Collection.Namespace, req.Collection.Name, timestamp)

	// Ensure backup directory exists
	backupPath := req.DestPath
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		return &pb.BackupCollectionResponse{
			Status: &pb.Status{
				Code:    pb.Status_INTERNAL,
				Message: fmt.Sprintf("failed to create backup directory: %v", err),
			},
		}, nil
	}

	// Backup database
	dbBackupPath := backupPath
	if err := bm.transport.Clone(ctx, sourceCollection, dbBackupPath); err != nil {
		return &pb.BackupCollectionResponse{
			Status: &pb.Status{
				Code:    pb.Status_INTERNAL,
				Message: fmt.Sprintf("failed to backup database: %v", err),
			},
		}, nil
	}

	// Count records
	recordCount, err := sourceCollection.Store.CountRecords(ctx)
	if err != nil {
		recordCount = 0 // Non-fatal
	}

	// Get backup size
	dbInfo, err := os.Stat(dbBackupPath)
	var sizeBytes int64
	if err == nil {
		sizeBytes = dbInfo.Size()
	}

	// Backup files if requested
	var fileCount int64
	if req.IncludeFiles && sourceCollection.FS != nil {
		filesDir := backupPath + ".files"
		if err := os.MkdirAll(filesDir, 0755); err != nil {
			// Clean up database backup
			os.Remove(dbBackupPath)
			return &pb.BackupCollectionResponse{
				Status: &pb.Status{
					Code:    pb.Status_INTERNAL,
					Message: fmt.Sprintf("failed to create files directory: %v", err),
				},
			}, nil
		}

		// Create filesystem for backup
		backupFS, err := local.NewFileSystem(filesDir)
		if err != nil {
			os.Remove(dbBackupPath)
			return &pb.BackupCollectionResponse{
				Status: &pb.Status{
					Code:    pb.Status_INTERNAL,
					Message: fmt.Sprintf("failed to create backup filesystem: %v", err),
				},
			}, nil
		}

		// Copy files
		filesBytes, err := CloneCollectionFiles(ctx, sourceCollection.FS, backupFS, "")
		if err != nil {
			os.Remove(dbBackupPath)
			os.RemoveAll(filesDir)
			return &pb.BackupCollectionResponse{
				Status: &pb.Status{
					Code:    pb.Status_INTERNAL,
					Message: fmt.Sprintf("failed to backup files: %v", err),
				},
			}, nil
		}

		sizeBytes += filesBytes

		// Count files
		files, err := backupFS.List(ctx, "")
		if err == nil {
			fileCount = int64(len(files))
		}
	}

	// Create backup metadata
	backupMeta := &pb.BackupMetadata{
		BackupId: backupID,
		Collection: &pb.NamespacedName{
			Namespace: req.Collection.Namespace,
			Name:      req.Collection.Name,
		},
		Timestamp:     timestamp,
		SizeBytes:     sizeBytes,
		RecordCount:   recordCount,
		FileCount:     fileCount,
		IncludesFiles: req.IncludeFiles,
		StoragePath:   backupPath,
		StorageType:   storageType,
		Metadata:      req.Metadata,
	}

	// Save metadata
	if err := bm.metaStore.SaveBackup(ctx, backupMeta); err != nil {
		// Clean up backup files
		os.Remove(dbBackupPath)
		if req.IncludeFiles {
			os.RemoveAll(backupPath + ".files")
		}
		return &pb.BackupCollectionResponse{
			Status: &pb.Status{
				Code:    pb.Status_INTERNAL,
				Message: fmt.Sprintf("failed to save backup metadata: %v", err),
			},
		}, nil
	}

	return &pb.BackupCollectionResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: "backup created successfully",
		},
		Backup:            backupMeta,
		BytesTransferred: sizeBytes,
	}, nil
}

// ListBackups lists available backups.
func (bm *BackupManager) ListBackups(ctx context.Context, req *pb.ListBackupsRequest) (*pb.ListBackupsResponse, error) {
	backups, totalCount, err := bm.metaStore.ListBackups(ctx, req)
	if err != nil {
		return &pb.ListBackupsResponse{
			Status: &pb.Status{
				Code:    pb.Status_INTERNAL,
				Message: fmt.Sprintf("failed to list backups: %v", err),
			},
		}, nil
	}

	return &pb.ListBackupsResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: fmt.Sprintf("found %d backups", len(backups)),
		},
		Backups:    backups,
		TotalCount: totalCount,
	}, nil
}

// RestoreBackup restores a collection from a backup.
func (bm *BackupManager) RestoreBackup(ctx context.Context, req *pb.RestoreBackupRequest) (*pb.RestoreBackupResponse, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Validate request
	if req.BackupId == "" {
		return &pb.RestoreBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_INVALID_ARGUMENT,
				Message: "backup_id is required",
			},
		}, nil
	}

	if req.DestNamespace == "" || req.DestName == "" {
		return &pb.RestoreBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_INVALID_ARGUMENT,
				Message: "dest_namespace and dest_name are required",
			},
		}, nil
	}

	// Get backup metadata
	backup, err := bm.metaStore.GetBackup(ctx, req.BackupId)
	if err != nil {
		return &pb.RestoreBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_NOT_FOUND,
				Message: fmt.Sprintf("backup not found: %v", err),
			},
		}, nil
	}

	// Check if backup file exists
	if _, err := os.Stat(backup.StoragePath); err != nil {
		return &pb.RestoreBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_NOT_FOUND,
				Message: fmt.Sprintf("backup file not found: %v", err),
			},
		}, nil
	}

	// Check if destination collection exists
	existingCollection, err := bm.repo.GetCollection(ctx, req.DestNamespace, req.DestName)
	if err == nil && !req.Overwrite {
		return &pb.RestoreBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_ALREADY_EXISTS,
				Message: "destination collection already exists (use overwrite=true to replace)",
			},
		}, nil
	}

	// If overwriting, remove existing database and files
	destDBPath := fmt.Sprintf("./data/collections/%s/%s/collection.db", req.DestNamespace, req.DestName)
	destFilesDir := fmt.Sprintf("./data/files/%s/%s", req.DestNamespace, req.DestName)
	if existingCollection != nil && req.Overwrite {
		// Close the existing collection's store if possible
		if existingCollection.Store != nil {
			existingCollection.Store.Close()
		}

		// Remove existing database and files
		os.Remove(destDBPath)
		os.RemoveAll(destFilesDir)
	}

	// Create destination database path
	if err := os.MkdirAll(filepath.Dir(destDBPath), 0755); err != nil {
		return &pb.RestoreBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_INTERNAL,
				Message: fmt.Sprintf("failed to create destination directory: %v", err),
			},
		}, nil
	}

	// Copy backup database to destination
	backupData, err := os.ReadFile(backup.StoragePath)
	if err != nil {
		return &pb.RestoreBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_INTERNAL,
				Message: fmt.Sprintf("failed to read backup: %v", err),
			},
		}, nil
	}

	if err := os.WriteFile(destDBPath, backupData, 0644); err != nil {
		return &pb.RestoreBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_INTERNAL,
				Message: fmt.Sprintf("failed to write restored database: %v", err),
			},
		}, nil
	}

	// Restore files if included
	var filesRestored int64
	if backup.IncludesFiles {
		filesDir := backup.StoragePath + ".files"
		if _, err := os.Stat(filesDir); err == nil {
			if err := os.MkdirAll(destFilesDir, 0755); err != nil {
				os.Remove(destDBPath)
				return &pb.RestoreBackupResponse{
					Status: &pb.Status{
						Code:    pb.Status_INTERNAL,
						Message: fmt.Sprintf("failed to create files directory: %v", err),
					},
				}, nil
			}

			// Copy files recursively using filesystem interfaces
			srcFS, err := NewLocalFileSystem(filesDir)
			if err != nil {
				os.Remove(destDBPath)
				os.RemoveAll(destFilesDir)
				return &pb.RestoreBackupResponse{
					Status: &pb.Status{
						Code:    pb.Status_INTERNAL,
						Message: fmt.Sprintf("failed to create source filesystem: %v", err),
					},
				}, nil
			}

			destFS, err := NewLocalFileSystem(destFilesDir)
			if err != nil {
				os.Remove(destDBPath)
				os.RemoveAll(destFilesDir)
				return &pb.RestoreBackupResponse{
					Status: &pb.Status{
						Code:    pb.Status_INTERNAL,
						Message: fmt.Sprintf("failed to create destination filesystem: %v", err),
					},
				}, nil
			}

			// Clone all files
			_, err = CloneCollectionFiles(ctx, srcFS, destFS, "")
			if err != nil {
				os.Remove(destDBPath)
				os.RemoveAll(destFilesDir)
				return &pb.RestoreBackupResponse{
					Status: &pb.Status{
						Code:    pb.Status_INTERNAL,
						Message: fmt.Sprintf("failed to restore files: %v", err),
					},
				}, nil
			}

			filesRestored = backup.FileCount
		}
	}

	// Create collection metadata in repo
	collectionMeta := &pb.Collection{
		Namespace: req.DestNamespace,
		Name:      req.DestName,
		MessageType: &pb.MessageTypeRef{
			MessageName: "RestoredFromBackup",
		},
		Metadata: &pb.Metadata{
			Labels: map[string]string{
				"restored_from_backup": req.BackupId,
				"original_collection":  fmt.Sprintf("%s/%s", backup.Collection.Namespace, backup.Collection.Name),
				"backup_timestamp":     fmt.Sprintf("%d", backup.Timestamp),
			},
		},
	}

	createResp, err := bm.repo.CreateCollection(ctx, collectionMeta)
	if err != nil || createResp.Status.Code != pb.Status_OK {
		// Clean up
		os.Remove(destDBPath)
		if backup.IncludesFiles {
			os.RemoveAll(fmt.Sprintf("./data/files/%s/%s", req.DestNamespace, req.DestName))
		}
		return &pb.RestoreBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_INTERNAL,
				Message: fmt.Sprintf("failed to create collection metadata: %v", err),
			},
		}, nil
	}

	return &pb.RestoreBackupResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: "backup restored successfully",
		},
		CollectionId:    createResp.CollectionId,
		RecordsRestored: backup.RecordCount,
		FilesRestored:   filesRestored,
	}, nil
}

// DeleteBackup deletes a backup.
func (bm *BackupManager) DeleteBackup(ctx context.Context, req *pb.DeleteBackupRequest) (*pb.DeleteBackupResponse, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Get backup metadata
	backup, err := bm.metaStore.GetBackup(ctx, req.BackupId)
	if err != nil {
		return &pb.DeleteBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_NOT_FOUND,
				Message: fmt.Sprintf("backup not found: %v", err),
			},
		}, nil
	}

	// Delete backup files
	var bytesFreed int64
	if info, err := os.Stat(backup.StoragePath); err == nil {
		bytesFreed = info.Size()
		os.Remove(backup.StoragePath)
	}

	// Delete files directory if it exists
	filesDir := backup.StoragePath + ".files"
	if _, err := os.Stat(filesDir); err == nil {
		os.RemoveAll(filesDir)
	}

	// Delete metadata
	if err := bm.metaStore.DeleteBackup(ctx, req.BackupId); err != nil {
		return &pb.DeleteBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_INTERNAL,
				Message: fmt.Sprintf("failed to delete backup metadata: %v", err),
			},
		}, nil
	}

	return &pb.DeleteBackupResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: "backup deleted successfully",
		},
		BytesFreed: bytesFreed,
	}, nil
}

// VerifyBackup verifies a backup's integrity.
func (bm *BackupManager) VerifyBackup(ctx context.Context, req *pb.VerifyBackupRequest) (*pb.VerifyBackupResponse, error) {
	// Get backup metadata
	backup, err := bm.metaStore.GetBackup(ctx, req.BackupId)
	if err != nil {
		return &pb.VerifyBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_NOT_FOUND,
				Message: fmt.Sprintf("backup not found: %v", err),
			},
			IsValid: false,
		}, nil
	}

	// Check if backup file exists
	if _, err := os.Stat(backup.StoragePath); err != nil {
		return &pb.VerifyBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_OK,
				Message: "backup file not found",
			},
			IsValid:      false,
			ErrorMessage: fmt.Sprintf("backup file missing: %v", err),
			Backup:       backup,
		}, nil
	}

	// Verify database can be opened (basic integrity check)
	dsn := fmt.Sprintf("file:%s?mode=ro", backup.StoragePath)
	testDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return &pb.VerifyBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_OK,
				Message: "backup database corrupted",
			},
			IsValid:      false,
			ErrorMessage: fmt.Sprintf("failed to open backup database: %v", err),
			Backup:       backup,
		}, nil
	}
	defer testDB.Close()

	// Run integrity check
	var integrityOk string
	if err := testDB.QueryRow("PRAGMA integrity_check").Scan(&integrityOk); err != nil {
		return &pb.VerifyBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_OK,
				Message: "backup integrity check failed",
			},
			IsValid:      false,
			ErrorMessage: fmt.Sprintf("integrity check error: %v", err),
			Backup:       backup,
		}, nil
	}

	if integrityOk != "ok" {
		return &pb.VerifyBackupResponse{
			Status: &pb.Status{
				Code:    pb.Status_OK,
				Message: "backup database corrupted",
			},
			IsValid:      false,
			ErrorMessage: fmt.Sprintf("integrity check failed: %s", integrityOk),
			Backup:       backup,
		}, nil
	}

	// If files are included, verify files directory
	if backup.IncludesFiles {
		filesDir := backup.StoragePath + ".files"
		if _, err := os.Stat(filesDir); err != nil {
			return &pb.VerifyBackupResponse{
				Status: &pb.Status{
					Code:    pb.Status_OK,
					Message: "backup files directory missing",
				},
				IsValid:      false,
				ErrorMessage: fmt.Sprintf("files directory missing: %v", err),
				Backup:       backup,
			}, nil
		}
	}

	return &pb.VerifyBackupResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: "backup is valid",
		},
		IsValid: true,
		Backup:  backup,
	}, nil
}

// Helper functions

func generateBackupID(namespace, name string, timestamp int64) string {
	data := fmt.Sprintf("%s/%s@%d", namespace, name, timestamp)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("backup-%s", hex.EncodeToString(hash[:])[:16])
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intToBool(i int) bool {
	return i != 0
}
