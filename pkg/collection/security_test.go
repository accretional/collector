package collection_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
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
			err := collection.ValidateName(tt.input, tt.fieldName)
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
		{"valid system", "system", false},     // system is valid - used for system collections
		{"valid internal", "internal", false}, // internal is valid
		{"valid admin", "admin", false},       // admin is valid
		{"valid metadata", "metadata", false}, // metadata is valid

		// Reserved namespaces (filesystem conflicts only)
		{"reserved repo", "repo", true},
		{"reserved backups", "backups", true},
		{"reserved files", "files", true},

		// Invalid patterns
		{"dot prefix", ".config", true},
		{"reserved .backup", ".backup", true}, // .backup is both dot-prefix AND reserved
		{"path traversal", "../etc", true},
		{"forward slash", "prod/staging", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := collection.ValidateNamespace(tt.namespace)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateNamespace(%q) error = %v, wantError %v", tt.namespace, err, tt.wantError)
			}
		})
	}
}

// TestPathConfigValidation tests that PathConfig methods validate inputs
func TestPathConfigValidation(t *testing.T) {
	pathConfig := collection.NewPathConfig(t.TempDir())

	tests := []struct {
		name      string
		namespace string
		collName  string
		wantError bool
	}{
		{"valid", "test", "collection", false},
		{"valid system namespace", "system", "collection", false},
		{"invalid namespace", "../etc", "collection", true},
		{"invalid collection", "test", "../passwd", true},
		{"reserved namespace repo", "repo", "collection", true},
		{"reserved namespace backups", "backups", "collection", true},
		{"reserved namespace files", "files", "collection", true},
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
	fs, err := collection.NewLocalFileSystem(root)
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
	fs, err := collection.NewLocalFileSystem(root)
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
	pathConfig := collection.NewPathConfig(tempDir)

	// Create registry store using CollectionRegistryStore (same as production)
	registryPath := filepath.Join(tempDir, "system", "collections.db")
	if err := os.MkdirAll(filepath.Dir(registryPath), 0755); err != nil {
		t.Fatalf("failed to create registry dir: %v", err)
	}

	registryDBStore, err := sqlite.NewStore(registryPath, collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatalf("failed to create registry db store: %v", err)
	}
	defer registryDBStore.Close()

	registryStore, err := collection.NewCollectionRegistryStoreFromStore(registryDBStore, &collection.LocalFileSystem{})
	if err != nil {
		t.Fatalf("failed to create registry store: %v", err)
	}
	defer registryStore.Close()

	// Create dummy store for repo - use real sqlite store
	dummyStore, err := sqlite.NewStore(":memory:", collection.Options{})
	if err != nil {
		t.Fatalf("failed to create dummy store: %v", err)
	}
	defer dummyStore.Close()

	// Create store factory that returns real stores
	storeFactory := func(path string, opts collection.Options) (collection.Store, error) {
		return sqlite.NewStore(path, opts)
	}

	repo := collection.NewCollectionRepo(dummyStore, pathConfig, registryStore, storeFactory)
	ctx := context.Background()

	tests := []struct {
		name      string
		namespace string
		collName  string
		wantError bool
	}{
		{"valid", "test", "mycollection", false},
		{"valid system namespace", "system", "mycollection", false},
		{"invalid namespace traversal", "../etc", "collection", true},
		{"invalid name traversal", "test", "../passwd", true},
		{"reserved namespace repo", "repo", "collection", true},
		{"reserved namespace backups", "backups", "collection", true},
		{"reserved namespace files", "files", "collection", true},
		{"invalid namespace slash", "test/prod", "collection", true},
		{"invalid name slash", "test", "coll/name", true},
		{"dot prefix namespace", ".hidden", "collection", true},
		{"dot prefix name", "test", ".secret", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coll := &pb.Collection{
				Namespace: tt.namespace,
				Name:      tt.collName,
			}

			_, err := repo.CreateCollection(ctx, coll)
			if (err != nil) != tt.wantError {
				t.Errorf("CreateCollection() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
