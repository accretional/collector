package collection

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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
