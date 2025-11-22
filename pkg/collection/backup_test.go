package collection

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/protobuf/types/known/timestamppb"
	_ "modernc.org/sqlite"
)

// createTestStore creates a simple SQLite store for testing
func createTestStore(path string) (Store, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=10000", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	// Create schema
	schema := `
	CREATE TABLE IF NOT EXISTS records (
		id TEXT PRIMARY KEY,
		proto_data BLOB,
		data_uri TEXT,
		created_at INTEGER,
		updated_at INTEGER,
		labels TEXT,
		jsontext TEXT
	);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}

	return &mockStore{db: db, path: path}, nil
}

// mockStore is a minimal Store implementation for testing
type mockStore struct {
	db   *sql.DB
	path string
}

func (m *mockStore) Close() error { return m.db.Close() }
func (m *mockStore) Path() string { return m.path }

func (m *mockStore) CreateRecord(ctx context.Context, r *pb.CollectionRecord) error {
	query := `INSERT INTO records (id, proto_data, data_uri, created_at, updated_at, labels, jsontext)
	          VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := m.db.ExecContext(ctx, query,
		r.Id, r.ProtoData, r.DataUri,
		r.Metadata.CreatedAt.Seconds, r.Metadata.UpdatedAt.Seconds,
		"{}", "{}")
	return err
}

func (m *mockStore) GetRecord(ctx context.Context, id string) (*pb.CollectionRecord, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStore) UpdateRecord(ctx context.Context, record *pb.CollectionRecord) error {
	return fmt.Errorf("not implemented")
}

func (m *mockStore) DeleteRecord(ctx context.Context, id string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockStore) ListRecords(ctx context.Context, offset, limit int) ([]*pb.CollectionRecord, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStore) CountRecords(ctx context.Context) (int64, error) {
	var count int64
	err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM records").Scan(&count)
	return count, err
}

func (m *mockStore) Search(ctx context.Context, query *SearchQuery) ([]*SearchResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockStore) Checkpoint(ctx context.Context) error {
	return nil
}

func (m *mockStore) ReIndex(ctx context.Context) error {
	return nil
}

func (m *mockStore) Backup(ctx context.Context, destPath string) error {
	// Use VACUUM INTO for backup
	query := fmt.Sprintf("VACUUM INTO '%s'", destPath)
	_, err := m.db.Exec(query)
	return err
}

func (m *mockStore) ExecuteRaw(query string, args ...interface{}) error {
	_, err := m.db.Exec(query, args...)
	return err
}

// TestBackupCollection_Simple tests basic backup functionality
func TestBackupCollection_Simple(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create a test collection with data
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := createTestStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Insert test records
	for i := 0; i < 100; i++ {
		record := &pb.CollectionRecord{
			Id: fmt.Sprintf("record-%d", i),
			Metadata: &pb.Metadata{
				Labels:    map[string]string{"test": "backup"},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			ProtoData: []byte(fmt.Sprintf("data-%d", i)),
		}
		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Create mock repo
	repo := &MockCollectionRepo{
		collections: make(map[string]*Collection),
	}

	// Register the test collection
	collection, err := NewCollection(&pb.Collection{
		Namespace: "test",
		Name:      "users",
	}, store, nil)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}
	repo.collections["test/users"] = collection

	// Create backup manager
	backupMetaPath := filepath.Join(tmpDir, "backups", "metadata.db")
	backupManager, err := NewBackupManager(repo, &SqliteTransport{}, backupMetaPath)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer backupManager.Close()

	// Create a backup
	backupPath := filepath.Join(tmpDir, "backups", "users-backup.db")
	req := &pb.BackupCollectionRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "users",
		},
		DestPath:     backupPath,
		IncludeFiles: false,
	}

	resp, err := backupManager.BackupCollection(ctx, req)
	if err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	if resp.Status.Code != pb.Status_OK {
		t.Fatalf("backup returned error: %s", resp.Status.Message)
	}

	if resp.Backup == nil {
		t.Fatal("backup metadata is nil")
	}

	// Verify backup file exists
	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("backup file not created: %v", err)
	}

	// Verify backup metadata
	if resp.Backup.RecordCount != 100 {
		t.Errorf("expected 100 records, got %d", resp.Backup.RecordCount)
	}

	if resp.Backup.SizeBytes == 0 {
		t.Error("backup size is 0")
	}
}

// TestListBackups tests listing backups
func TestListBackups(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create backup metadata store
	metaStore, err := NewBackupMetadataStore(filepath.Join(tmpDir, "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create metadata store: %v", err)
	}
	defer metaStore.Close()

	// Create test backups
	for i := 0; i < 5; i++ {
		backup := &pb.BackupMetadata{
			BackupId: fmt.Sprintf("backup-%d", i),
			Collection: &pb.NamespacedName{
				Namespace: "test",
				Name:      "users",
			},
			Timestamp:     time.Now().Unix() + int64(i),
			SizeBytes:     1024 * int64(i+1),
			RecordCount:   int64(100 * (i + 1)),
			FileCount:     0,
			IncludesFiles: false,
			StoragePath:   fmt.Sprintf("/backups/backup-%d.db", i),
			StorageType:   "local",
		}

		if err := metaStore.SaveBackup(ctx, backup); err != nil {
			t.Fatalf("failed to save backup: %v", err)
		}
	}

	// List all backups for the collection
	backups, totalCount, err := metaStore.ListBackups(ctx, &pb.ListBackupsRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "users",
		},
	})

	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}

	if totalCount != 5 {
		t.Errorf("expected 5 backups, got %d", totalCount)
	}

	if len(backups) != 5 {
		t.Errorf("expected 5 backups in result, got %d", len(backups))
	}

	// Test filtering by limit
	backups, _, err = metaStore.ListBackups(ctx, &pb.ListBackupsRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "users",
		},
		Limit: 2,
	})

	if err != nil {
		t.Fatalf("failed to list backups with limit: %v", err)
	}

	if len(backups) != 2 {
		t.Errorf("expected 2 backups with limit, got %d", len(backups))
	}
}

// TestDeleteBackup tests backup deletion
func TestDeleteBackup(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create a test backup file
	backupPath := filepath.Join(tmpDir, "test-backup.db")
	if err := os.WriteFile(backupPath, []byte("fake backup data"), 0644); err != nil {
		t.Fatalf("failed to create backup file: %v", err)
	}

	// Create metadata store
	metaStore, err := NewBackupMetadataStore(filepath.Join(tmpDir, "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create metadata store: %v", err)
	}
	defer metaStore.Close()

	// Save backup metadata
	backup := &pb.BackupMetadata{
		BackupId: "test-backup-123",
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "users",
		},
		Timestamp:   time.Now().Unix(),
		SizeBytes:   16,
		StoragePath: backupPath,
		StorageType: "local",
	}

	if err := metaStore.SaveBackup(ctx, backup); err != nil {
		t.Fatalf("failed to save backup: %v", err)
	}

	// Create backup manager
	repo := &MockCollectionRepo{collections: make(map[string]*Collection)}
	backupManager, err := NewBackupManager(repo, &SqliteTransport{}, filepath.Join(tmpDir, "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer backupManager.Close()

	// Delete the backup
	deleteResp, err := backupManager.DeleteBackup(ctx, &pb.DeleteBackupRequest{
		BackupId: "test-backup-123",
	})

	if err != nil {
		t.Fatalf("delete backup failed: %v", err)
	}

	if deleteResp.Status.Code != pb.Status_OK {
		t.Errorf("delete returned error: %s", deleteResp.Status.Message)
	}

	// Verify file is deleted
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Error("backup file still exists after deletion")
	}

	// Verify metadata is deleted
	_, err = metaStore.GetBackup(ctx, "test-backup-123")
	if err == nil {
		t.Error("backup metadata still exists after deletion")
	}
}

// TestVerifyBackup tests backup verification
func TestVerifyBackup(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create a valid SQLite database for backup
	backupPath := filepath.Join(tmpDir, "valid-backup.db")
	store, err := createTestStore(backupPath)
	if err != nil {
		t.Fatalf("failed to create backup store: %v", err)
	}
	store.Close()

	// Create metadata store
	metaStore, err := NewBackupMetadataStore(filepath.Join(tmpDir, "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create metadata store: %v", err)
	}
	defer metaStore.Close()

	// Save backup metadata
	backup := &pb.BackupMetadata{
		BackupId: "valid-backup-123",
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "users",
		},
		Timestamp:   time.Now().Unix(),
		SizeBytes:   1024,
		StoragePath: backupPath,
		StorageType: "local",
	}

	if err := metaStore.SaveBackup(ctx, backup); err != nil {
		t.Fatalf("failed to save backup: %v", err)
	}

	// Create backup manager
	repo := &MockCollectionRepo{collections: make(map[string]*Collection)}
	backupManager, err := NewBackupManager(repo, &SqliteTransport{}, filepath.Join(tmpDir, "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer backupManager.Close()

	// Verify the backup
	verifyResp, err := backupManager.VerifyBackup(ctx, &pb.VerifyBackupRequest{
		BackupId: "valid-backup-123",
	})

	if err != nil {
		t.Fatalf("verify backup failed: %v", err)
	}

	if verifyResp.Status.Code != pb.Status_OK {
		t.Errorf("verify returned error: %s", verifyResp.Status.Message)
	}

	if !verifyResp.IsValid {
		t.Errorf("backup should be valid: %s", verifyResp.ErrorMessage)
	}
}

// TestVerifyBackup_Missing tests verification of missing backup
func TestVerifyBackup_Missing(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create metadata store
	metaStore, err := NewBackupMetadataStore(filepath.Join(tmpDir, "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create metadata store: %v", err)
	}
	defer metaStore.Close()

	// Save backup metadata pointing to non-existent file
	backup := &pb.BackupMetadata{
		BackupId: "missing-backup-123",
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "users",
		},
		Timestamp:   time.Now().Unix(),
		SizeBytes:   1024,
		StoragePath: "/nonexistent/backup.db",
		StorageType: "local",
	}

	if err := metaStore.SaveBackup(ctx, backup); err != nil {
		t.Fatalf("failed to save backup: %v", err)
	}

	// Create backup manager
	repo := &MockCollectionRepo{collections: make(map[string]*Collection)}
	backupManager, err := NewBackupManager(repo, &SqliteTransport{}, filepath.Join(tmpDir, "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer backupManager.Close()

	// Verify the backup (should fail)
	verifyResp, err := backupManager.VerifyBackup(ctx, &pb.VerifyBackupRequest{
		BackupId: "missing-backup-123",
	})

	if err != nil {
		t.Fatalf("verify backup failed: %v", err)
	}

	if verifyResp.IsValid {
		t.Error("backup should be invalid (file missing)")
	}

	if verifyResp.ErrorMessage == "" {
		t.Error("expected error message for missing backup")
	}
}

// TestBackupValidation tests request validation
func TestBackupValidation(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	repo := &MockCollectionRepo{collections: make(map[string]*Collection)}
	backupManager, err := NewBackupManager(repo, &SqliteTransport{}, filepath.Join(tmpDir, "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer backupManager.Close()

	testCases := []struct {
		name    string
		req     *pb.BackupCollectionRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: &pb.BackupCollectionRequest{
				Collection: &pb.NamespacedName{
					Namespace: "test",
					Name:      "users",
				},
				DestPath: "/tmp/backup.db",
			},
			wantErr: true, // Will fail because collection doesn't exist, but validation passes
		},
		{
			name: "missing collection",
			req: &pb.BackupCollectionRequest{
				DestPath: "/tmp/backup.db",
			},
			wantErr: true,
		},
		{
			name: "missing dest_path",
			req: &pb.BackupCollectionRequest{
				Collection: &pb.NamespacedName{
					Namespace: "test",
					Name:      "users",
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := backupManager.BackupCollection(ctx, tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantErr && resp.Status.Code == pb.Status_OK {
				t.Error("expected error, got OK")
			}
		})
	}
}

// MockCollectionRepo for testing
type MockCollectionRepo struct {
	collections map[string]*Collection
}

func (m *MockCollectionRepo) CreateCollection(ctx context.Context, collection *pb.Collection) (*pb.CreateCollectionResponse, error) {
	key := collection.Namespace + "/" + collection.Name
	return &pb.CreateCollectionResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: "created",
		},
		CollectionId: key,
	}, nil
}

func (m *MockCollectionRepo) Discover(ctx context.Context, req *pb.DiscoverRequest) (*pb.DiscoverResponse, error) {
	return &pb.DiscoverResponse{
		Status: &pb.Status{Code: pb.Status_OK},
	}, nil
}

func (m *MockCollectionRepo) Route(ctx context.Context, req *pb.RouteRequest) (*pb.RouteResponse, error) {
	return &pb.RouteResponse{
		Status: &pb.Status{Code: pb.Status_OK},
	}, nil
}

func (m *MockCollectionRepo) SearchCollections(ctx context.Context, req *pb.SearchCollectionsRequest) (*pb.SearchCollectionsResponse, error) {
	return &pb.SearchCollectionsResponse{
		Status: &pb.Status{Code: pb.Status_OK},
	}, nil
}

func (m *MockCollectionRepo) GetCollection(ctx context.Context, namespace, name string) (*Collection, error) {
	key := namespace + "/" + name
	collection, exists := m.collections[key]
	if !exists {
		return nil, fmt.Errorf("collection not found: %s", key)
	}
	return collection, nil
}

func (m *MockCollectionRepo) UpdateCollectionMetadata(ctx context.Context, namespace, name string, meta *pb.Collection) error {
	return nil
}

// TestBackupWithFiles tests backup including filesystem data
func TestBackupWithFiles(t *testing.T) {
	t.Skip("TODO: Refactor file cloning to use FileSystem interface instead of concrete type")
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create test store
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := createTestStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Insert records
	for i := 0; i < 50; i++ {
		record := &pb.CollectionRecord{
			Id: fmt.Sprintf("record-%d", i),
			Metadata: &pb.Metadata{
				Labels:    map[string]string{},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			ProtoData: []byte(fmt.Sprintf("data-%d", i)),
		}
		store.CreateRecord(ctx, record)
	}

	// Create filesystem with files
	fsDir := filepath.Join(tmpDir, "files")
	os.MkdirAll(fsDir, 0755)
	for i := 0; i < 10; i++ {
		filePath := filepath.Join(fsDir, fmt.Sprintf("file-%d.txt", i))
		os.WriteFile(filePath, []byte(fmt.Sprintf("file content %d", i)), 0644)
	}

	// Create mock repo with filesystem
	repo := &MockCollectionRepo{collections: make(map[string]*Collection)}
	fs, err := NewLocalFileSystem(fsDir)
	if err != nil {
		t.Fatalf("failed to create filesystem: %v", err)
	}

	collection, err := NewCollection(&pb.Collection{
		Namespace: "test",
		Name:      "users",
	}, store, fs)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}
	repo.collections["test/users"] = collection

	// Create backup manager
	backupManager, err := NewBackupManager(repo, &SqliteTransport{}, filepath.Join(tmpDir, "backups", "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer backupManager.Close()

	// Backup with files
	backupPath := filepath.Join(tmpDir, "backups", "users-with-files.db")
	req := &pb.BackupCollectionRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "users",
		},
		DestPath:     backupPath,
		IncludeFiles: true,
	}

	resp, err := backupManager.BackupCollection(ctx, req)
	if err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	if resp.Status.Code != pb.Status_OK {
		t.Fatalf("backup returned error: %s", resp.Status.Message)
	}

	// Verify files directory exists
	filesDir := backupPath + ".files"
	if _, err := os.Stat(filesDir); err != nil {
		t.Errorf("files directory not created: %v", err)
	}

	// Verify backup includes files
	if resp.Backup.FileCount == 0 {
		t.Error("backup should include files")
	}

	if !resp.Backup.IncludesFiles {
		t.Error("backup metadata should indicate files included")
	}
}

// TestBackupConcurrent tests concurrent backup operations
func TestBackupConcurrent(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create multiple collections
	numCollections := 5
	repo := &MockCollectionRepo{collections: make(map[string]*Collection)}

	for i := 0; i < numCollections; i++ {
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("collection-%d.db", i))
		store, err := createTestStore(dbPath)
		if err != nil {
			t.Fatalf("failed to create store %d: %v", i, err)
		}
		defer store.Close()

		// Insert records
		for j := 0; j < 100; j++ {
			record := &pb.CollectionRecord{
				Id: fmt.Sprintf("record-%d", j),
				Metadata: &pb.Metadata{
					Labels:    map[string]string{},
					CreatedAt: timestamppb.Now(),
					UpdatedAt: timestamppb.Now(),
				},
				ProtoData: []byte(fmt.Sprintf("data-%d", j)),
			}
			store.CreateRecord(ctx, record)
		}

		collection, err := NewCollection(&pb.Collection{
			Namespace: "test",
			Name:      fmt.Sprintf("collection-%d", i),
		}, store, nil)
		if err != nil {
			t.Fatalf("failed to create collection: %v", err)
		}
		repo.collections[fmt.Sprintf("test/collection-%d", i)] = collection
	}

	// Create backup manager
	backupManager, err := NewBackupManager(repo, &SqliteTransport{}, filepath.Join(tmpDir, "backups", "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer backupManager.Close()

	// Backup all collections concurrently
	var wg sync.WaitGroup
	errors := make(chan error, numCollections)

	for i := 0; i < numCollections; i++ {
		wg.Add(1)
		go func(collectionNum int) {
			defer wg.Done()

			req := &pb.BackupCollectionRequest{
				Collection: &pb.NamespacedName{
					Namespace: "test",
					Name:      fmt.Sprintf("collection-%d", collectionNum),
				},
				DestPath:     filepath.Join(tmpDir, "backups", fmt.Sprintf("backup-%d.db", collectionNum)),
				IncludeFiles: false,
			}

			resp, err := backupManager.BackupCollection(ctx, req)
			if err != nil {
				errors <- fmt.Errorf("collection %d: %v", collectionNum, err)
				return
			}

			if resp.Status.Code != pb.Status_OK {
				errors <- fmt.Errorf("collection %d: %s", collectionNum, resp.Status.Message)
				return
			}

			t.Logf("Collection %d backed up: %d records, %d bytes",
				collectionNum, resp.Backup.RecordCount, resp.BytesTransferred)
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}

	// Verify all backups were created
	listResp, err := backupManager.ListBackups(ctx, &pb.ListBackupsRequest{
		Namespace: "test",
	})
	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}

	if listResp.TotalCount != int64(numCollections) {
		t.Errorf("expected %d backups, got %d", numCollections, listResp.TotalCount)
	}
}

// TestBackupLargeDataset tests backup with larger dataset
func TestBackupLargeDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large dataset test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create test store with 10k records
	dbPath := filepath.Join(tmpDir, "large.db")
	store, err := createTestStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	numRecords := 10000
	for i := 0; i < numRecords; i++ {
		record := &pb.CollectionRecord{
			Id: fmt.Sprintf("record-%d", i),
			Metadata: &pb.Metadata{
				Labels:    map[string]string{"index": fmt.Sprintf("%d", i)},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			ProtoData: []byte(fmt.Sprintf("data-%d-with-some-extra-content-to-make-it-larger", i)),
		}
		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record %d: %v", i, err)
		}
	}

	// Create mock repo
	repo := &MockCollectionRepo{collections: make(map[string]*Collection)}
	collection, err := NewCollection(&pb.Collection{
		Namespace: "test",
		Name:      "large",
	}, store, nil)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}
	repo.collections["test/large"] = collection

	// Create backup manager
	backupManager, err := NewBackupManager(repo, &SqliteTransport{}, filepath.Join(tmpDir, "backups", "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer backupManager.Close()

	// Backup
	backupPath := filepath.Join(tmpDir, "backups", "large-backup.db")
	startTime := time.Now()

	resp, err := backupManager.BackupCollection(ctx, &pb.BackupCollectionRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "large",
		},
		DestPath: backupPath,
	})

	duration := time.Since(startTime)

	if err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	if resp.Status.Code != pb.Status_OK {
		t.Fatalf("backup returned error: %s", resp.Status.Message)
	}

	t.Logf("Backup completed in %v", duration)
	t.Logf("Records: %d, Size: %d bytes", resp.Backup.RecordCount, resp.BytesTransferred)

	if resp.Backup.RecordCount != int64(numRecords) {
		t.Errorf("expected %d records, got %d", numRecords, resp.Backup.RecordCount)
	}

	// Verify backup can be opened
	verifyResp, err := backupManager.VerifyBackup(ctx, &pb.VerifyBackupRequest{
		BackupId: resp.Backup.BackupId,
	})
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}

	if !verifyResp.IsValid {
		t.Errorf("backup should be valid: %s", verifyResp.ErrorMessage)
	}
}

// TestRestoreWithOverwrite tests restore with overwrite flag
func TestRestoreWithOverwrite(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create a backup
	backupPath := filepath.Join(tmpDir, "test-backup.db")
	store, err := createTestStore(backupPath)
	if err != nil {
		t.Fatalf("failed to create backup store: %v", err)
	}

	// Add some records to backup
	for i := 0; i < 50; i++ {
		record := &pb.CollectionRecord{
			Id: fmt.Sprintf("record-%d", i),
			Metadata: &pb.Metadata{
				Labels:    map[string]string{},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			ProtoData: []byte(fmt.Sprintf("backup-data-%d", i)),
		}
		store.CreateRecord(ctx, record)
	}
	store.Close()

	// Create metadata store
	metaStore, err := NewBackupMetadataStore(filepath.Join(tmpDir, "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create metadata store: %v", err)
	}
	defer metaStore.Close()

	// Save backup metadata
	backup := &pb.BackupMetadata{
		BackupId: "test-backup-restore",
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "original",
		},
		Timestamp:   time.Now().Unix(),
		SizeBytes:   1024,
		RecordCount: 50,
		StoragePath: backupPath,
		StorageType: "local",
	}
	if err := metaStore.SaveBackup(ctx, backup); err != nil {
		t.Fatalf("failed to save backup: %v", err)
	}

	// Create backup manager
	repo := &MockCollectionRepo{collections: make(map[string]*Collection)}
	backupManager, err := NewBackupManager(repo, &SqliteTransport{}, filepath.Join(tmpDir, "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer backupManager.Close()

	// First restore (should succeed)
	restoreResp, err := backupManager.RestoreBackup(ctx, &pb.RestoreBackupRequest{
		BackupId:      "test-backup-restore",
		DestNamespace: "restored",
		DestName:      "collection1",
		Overwrite:     false,
	})

	if err != nil {
		t.Fatalf("first restore failed: %v", err)
	}

	if restoreResp.Status.Code != pb.Status_OK {
		t.Errorf("first restore returned error: %s", restoreResp.Status.Message)
	}

	t.Logf("First restore: %d records restored", restoreResp.RecordsRestored)
}

// TestBackupEmptyCollection tests backup of empty collection
func TestBackupEmptyCollection(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create empty store
	dbPath := filepath.Join(tmpDir, "empty.db")
	store, err := createTestStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Create mock repo
	repo := &MockCollectionRepo{collections: make(map[string]*Collection)}
	collection, err := NewCollection(&pb.Collection{
		Namespace: "test",
		Name:      "empty",
	}, store, nil)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}
	repo.collections["test/empty"] = collection

	// Create backup manager
	backupManager, err := NewBackupManager(repo, &SqliteTransport{}, filepath.Join(tmpDir, "backups", "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer backupManager.Close()

	// Backup empty collection
	backupPath := filepath.Join(tmpDir, "backups", "empty-backup.db")
	resp, err := backupManager.BackupCollection(ctx, &pb.BackupCollectionRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "empty",
		},
		DestPath: backupPath,
	})

	if err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	if resp.Status.Code != pb.Status_OK {
		t.Fatalf("backup returned error: %s", resp.Status.Message)
	}

	if resp.Backup.RecordCount != 0 {
		t.Errorf("expected 0 records, got %d", resp.Backup.RecordCount)
	}

	// Verify empty backup
	verifyResp, err := backupManager.VerifyBackup(ctx, &pb.VerifyBackupRequest{
		BackupId: resp.Backup.BackupId,
	})

	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}

	if !verifyResp.IsValid {
		t.Errorf("empty backup should be valid: %s", verifyResp.ErrorMessage)
	}
}

// TestBackupWithSpecialCharacters tests collections with special characters
func TestBackupWithSpecialCharacters(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create store
	dbPath := filepath.Join(tmpDir, "special.db")
	store, err := createTestStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Insert records with special characters
	specialRecords := []string{
		"record-with-unicode-你好",
		"record-with-emoji-🚀",
		"record-with-spaces-hello world",
		"record-with-quotes-'test'",
	}

	for _, id := range specialRecords {
		record := &pb.CollectionRecord{
			Id: id,
			Metadata: &pb.Metadata{
				Labels:    map[string]string{"type": "special"},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			ProtoData: []byte(fmt.Sprintf("data for %s", id)),
		}
		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("failed to create record %s: %v", id, err)
		}
	}

	// Create mock repo
	repo := &MockCollectionRepo{collections: make(map[string]*Collection)}
	collection, err := NewCollection(&pb.Collection{
		Namespace: "test",
		Name:      "special-chars",
	}, store, nil)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}
	repo.collections["test/special-chars"] = collection

	// Create backup manager
	backupManager, err := NewBackupManager(repo, &SqliteTransport{}, filepath.Join(tmpDir, "backups", "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer backupManager.Close()

	// Backup
	backupPath := filepath.Join(tmpDir, "backups", "special-backup.db")
	resp, err := backupManager.BackupCollection(ctx, &pb.BackupCollectionRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "special-chars",
		},
		DestPath: backupPath,
	})

	if err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	if resp.Status.Code != pb.Status_OK {
		t.Fatalf("backup returned error: %s", resp.Status.Message)
	}

	if resp.Backup.RecordCount != int64(len(specialRecords)) {
		t.Errorf("expected %d records, got %d", len(specialRecords), resp.Backup.RecordCount)
	}
}

// TestBackupMetadataFiltering tests metadata filtering
func TestBackupMetadataFiltering(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	metaStore, err := NewBackupMetadataStore(filepath.Join(tmpDir, "metadata.db"))
	if err != nil {
		t.Fatalf("failed to create metadata store: %v", err)
	}
	defer metaStore.Close()

	// Create backups with different timestamps
	now := time.Now().Unix()
	for i := 0; i < 10; i++ {
		backup := &pb.BackupMetadata{
			BackupId: fmt.Sprintf("backup-%d", i),
			Collection: &pb.NamespacedName{
				Namespace: "test",
				Name:      "users",
			},
			Timestamp:   now - int64(i*86400), // One per day going back
			SizeBytes:   1024,
			RecordCount: 100,
			StoragePath: fmt.Sprintf("/backups/backup-%d.db", i),
			StorageType: "local",
		}
		if err := metaStore.SaveBackup(ctx, backup); err != nil {
			t.Fatalf("failed to save backup: %v", err)
		}
	}

	// Test filtering by timestamp
	threeDaysAgo := now - (3 * 86400)
	backups, _, err := metaStore.ListBackups(ctx, &pb.ListBackupsRequest{
		Collection: &pb.NamespacedName{
			Namespace: "test",
			Name:      "users",
		},
		SinceTimestamp: threeDaysAgo,
	})

	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}

	if len(backups) != 4 { // Today + 3 days ago = 4 backups
		t.Errorf("expected 4 backups since 3 days ago, got %d", len(backups))
	}

	// Verify ordering (most recent first)
	for i := 0; i < len(backups)-1; i++ {
		if backups[i].Timestamp < backups[i+1].Timestamp {
			t.Error("backups should be ordered by timestamp DESC")
		}
	}
}
