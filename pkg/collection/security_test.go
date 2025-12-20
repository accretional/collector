package collection

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
)

// TestValidateName tests the name validation function
func TestValidateName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		fieldName string
		wantError bool
	}{
		// Valid names
		{"valid simple", "myname", "name", false},
		{"valid with dash", "my-name", "name", false},
		{"valid with underscore", "my_name", "name", false},
		{"valid with numbers", "name123", "name", false},

		// Invalid names
		{"empty", "", "name", true},
		{"dot prefix", ".hidden", "name", true},
		{"forward slash", "path/to/file", "name", true},
		{"backslash", "path\\to\\file", "name", true},
		{"dot dot", "..", "name", true},
		{"dot dot in middle", "foo..bar", "name", true},
		{"colon", "C:Users", "name", true},
		{"asterisk", "wild*card", "name", true},
		{"question mark", "what?", "name", true},
		{"double quote", "quo\"te", "name", true},
		{"less than", "<script>", "name", true},
		{"greater than", "tag>", "name", true},
		{"pipe", "cmd|grep", "name", true},
		{"whitespace only", "   ", "name", true},
		{"too long", strings.Repeat("a", 256), "name", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input, tt.fieldName)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateName(%q) error = %v, wantError %v", tt.input, err, tt.wantError)
			}
		})
	}
}

// TestValidateNamespace tests namespace-specific validation including reserved names
func TestValidateNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		wantError bool
	}{
		// Valid namespaces
		{"valid simple", "production", false},
		{"valid with dash", "prod-v2", false},
		{"valid with underscore", "prod_env", false},

		// Reserved namespaces
		{"reserved repo", "repo", true},
		{"reserved backups", "backups", true},
		{"reserved .backup", ".backup", true},
		{"reserved files", "files", true},
		{"reserved system", "system", true},
		{"reserved internal", "internal", true},
		{"reserved admin", "admin", true},
		{"reserved metadata", "metadata", true},

		// Invalid patterns
		{"dot prefix", ".config", true},
		{"path traversal", "../etc", true},
		{"forward slash", "prod/staging", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateNamespace(%q) error = %v, wantError %v", tt.namespace, err, tt.wantError)
			}
		})
	}
}

// TestPathConfigValidation tests that PathConfig methods validate inputs
func TestPathConfigValidation(t *testing.T) {
	pathConfig := NewPathConfig(t.TempDir())

	tests := []struct {
		name      string
		namespace string
		collName  string
		wantError bool
	}{
		{"valid", "test", "collection", false},
		{"invalid namespace", "../etc", "collection", true},
		{"invalid collection", "test", "../passwd", true},
		{"reserved namespace", "system", "collection", true},
		{"dot prefix namespace", ".hidden", "collection", true},
		{"dot prefix collection", "test", ".secret", true},
		{"slash in namespace", "test/prod", "collection", true},
		{"slash in collection", "test", "coll/name", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test CollectionDBPath
			_, err := pathConfig.CollectionDBPath(tt.namespace, tt.collName)
			if (err != nil) != tt.wantError {
				t.Errorf("CollectionDBPath() error = %v, wantError %v", err, tt.wantError)
			}

			// Test CollectionFilesPath
			_, err = pathConfig.CollectionFilesPath(tt.namespace, tt.collName)
			if (err != nil) != tt.wantError {
				t.Errorf("CollectionFilesPath() error = %v, wantError %v", err, tt.wantError)
			}

			// Test BackupPath
			_, err = pathConfig.BackupPath(tt.namespace, tt.collName, 123456789)
			if (err != nil) != tt.wantError {
				t.Errorf("BackupPath() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// TestFileSystemPathTraversal tests that filesystem operations reject path traversal attempts
func TestFileSystemPathTraversal(t *testing.T) {
	root := t.TempDir()
	fs, err := NewLocalFileSystem(root)
	if err != nil {
		t.Fatalf("failed to create filesystem: %v", err)
	}

	ctx := context.Background()
	testData := []byte("test content")

	tests := []struct {
		name string
		path string
	}{
		{"dot dot", "../etc/passwd"},
		{"absolute path", "/etc/passwd"},
		{"multiple dot dot", "../../../../../../etc/passwd"},
		{"dot dot in middle", "safe/../../../etc/passwd"},
		{"encoded dot dot", "..%2F..%2Fetc%2Fpasswd"},
		{"backslash traversal", "..\\..\\windows\\system32"},
		{"mixed slashes", "../..\\etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test Save (write)
			err := fs.Save(ctx, tt.path, testData)
			if err == nil {
				t.Errorf("Save() should have rejected path %q but succeeded", tt.path)
			}

			// Test Load (read)
			_, err = fs.Load(ctx, tt.path)
			if err == nil {
				t.Errorf("Load() should have rejected path %q but succeeded", tt.path)
			}

			// Test Delete
			err = fs.Delete(ctx, tt.path)
			if err == nil {
				t.Errorf("Delete() should have rejected path %q but succeeded", tt.path)
			}

			// Test List
			_, err = fs.List(ctx, tt.path)
			if err == nil {
				t.Errorf("List() should have rejected path %q but succeeded", tt.path)
			}

			// Test Exists
			_, err = fs.Exists(ctx, tt.path)
			if err == nil {
				t.Errorf("Exists() should have rejected path %q but succeeded", tt.path)
			}
		})
	}
}

// TestFileSystemSafePaths tests that safe paths work correctly
func TestFileSystemSafePaths(t *testing.T) {
	root := t.TempDir()
	fs, err := NewLocalFileSystem(root)
	if err != nil {
		t.Fatalf("failed to create filesystem: %v", err)
	}

	ctx := context.Background()
	testData := []byte("test content")

	safePaths := []string{
		"file.txt",
		"dir/file.txt",
		"deep/nested/dir/file.txt",
		"file-with-dash.txt",
		"file_with_underscore.txt",
	}

	for _, path := range safePaths {
		t.Run(path, func(t *testing.T) {
			// Save should work
			err := fs.Save(ctx, path, testData)
			if err != nil {
				t.Errorf("Save() failed for safe path %q: %v", path, err)
			}

			// Load should work
			content, err := fs.Load(ctx, path)
			if err != nil {
				t.Errorf("Load() failed for safe path %q: %v", path, err)
			}
			if string(content) != string(testData) {
				t.Errorf("Load() returned wrong content for %q", path)
			}

			// Exists should work
			exists, err := fs.Exists(ctx, path)
			if err != nil {
				t.Errorf("Exists() failed for safe path %q: %v", path, err)
			}
			if !exists {
				t.Errorf("Exists() returned false for existing file %q", path)
			}

			// Delete should work
			err = fs.Delete(ctx, path)
			if err != nil {
				t.Errorf("Delete() failed for safe path %q: %v", path, err)
			}
		})
	}
}

// TestCreateCollectionValidation tests that CreateCollection validates namespace and name
func TestCreateCollectionValidation(t *testing.T) {
	tempDir := t.TempDir()
	pathConfig := NewPathConfig(tempDir)

	// Create registry store
	registryPath := filepath.Join(tempDir, "registry.db")
	registryStore, err := NewSqliteRegistryStore(registryPath)
	if err != nil {
		t.Fatalf("failed to create registry store: %v", err)
	}
	defer registryStore.Close()

	// Create dummy store for repo
	dummyStore := &mockStore{}
	storeFactory := func(path string, opts Options) (Store, error) {
		return &mockStore{}, nil
	}

	repo := NewCollectionRepo(dummyStore, pathConfig, registryStore, storeFactory)
	ctx := context.Background()

	tests := []struct {
		name      string
		namespace string
		collName  string
		wantError bool
	}{
		{"valid", "test", "mycollection", false},
		{"invalid namespace traversal", "../etc", "collection", true},
		{"invalid name traversal", "test", "../passwd", true},
		{"reserved namespace", "system", "collection", true},
		{"reserved namespace repo", "repo", "collection", true},
		{"invalid namespace slash", "test/prod", "collection", true},
		{"invalid name slash", "test", "coll/name", true},
		{"dot prefix namespace", ".hidden", "collection", true},
		{"dot prefix name", "test", ".secret", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collection := &pb.Collection{
				Namespace: tt.namespace,
				Name:      tt.collName,
			}

			_, err := repo.CreateCollection(ctx, collection)
			if (err != nil) != tt.wantError {
				t.Errorf("CreateCollection() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// mockStore is a minimal Store implementation for testing
type mockStore struct{}

func (m *mockStore) Close() error                                                    { return nil }
func (m *mockStore) Path() string                                                    { return ":memory:" }
func (m *mockStore) CreateRecord(ctx context.Context, record *pb.CollectionRecord) error { return nil }
func (m *mockStore) GetRecord(ctx context.Context, id string) (*pb.CollectionRecord, error) {
	return nil, nil
}
func (m *mockStore) UpdateRecord(ctx context.Context, record *pb.CollectionRecord) error { return nil }
func (m *mockStore) DeleteRecord(ctx context.Context, id string) error                   { return nil }
func (m *mockStore) ListRecords(ctx context.Context, offset, limit int) ([]*pb.CollectionRecord, error) {
	return nil, nil
}
func (m *mockStore) CountRecords(ctx context.Context) (int64, error) { return 0, nil }
func (m *mockStore) Search(ctx context.Context, query *SearchQuery) ([]*SearchResult, error) {
	return nil, nil
}
func (m *mockStore) Checkpoint(ctx context.Context) error          { return nil }
func (m *mockStore) ReIndex(ctx context.Context) error             { return nil }
func (m *mockStore) Backup(ctx context.Context, destPath string) error { return nil }
func (m *mockStore) ExecuteRaw(query string, args ...interface{}) error { return nil }
