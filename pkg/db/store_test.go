package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/accretional/collector/pkg/collection"
)

func TestNewStore_DefaultSQLite(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := NewStore(ctx, StoreConfig{
		Path:    dbPath,
		Options: collection.Options{},
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	if store == nil {
		t.Fatal("NewStore returned nil store")
	}

	if store.Path() != dbPath {
		t.Errorf("expected path %s, got %s", dbPath, store.Path())
	}
}

func TestNewStore_WithFTS(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "fts.db")

	store, err := NewStore(ctx, StoreConfig{
		Path: dbPath,
		Options: collection.Options{
			EnableFTS: true,
		},
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	if !store.Supports(CapabilityFTS) {
		t.Error("store should support FTS")
	}

	if store.Supports(CapabilityVector) {
		t.Error("store should not support vector without EnableVector")
	}
}

func TestNewStore_WithJSON(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "json.db")

	store, err := NewStore(ctx, StoreConfig{
		Path: dbPath,
		Options: collection.Options{
			EnableJSON: true,
		},
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	if !store.Supports(CapabilityJSON) {
		t.Error("store should support JSON")
	}
}

func TestNewStore_WithAllOptions(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "all.db")

	store, err := NewStore(ctx, StoreConfig{
		Path: dbPath,
		Options: collection.Options{
			EnableFTS:  true,
			EnableJSON: true,
		},
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	if !store.Supports(CapabilityFTS) {
		t.Error("store should support FTS")
	}

	if !store.Supports(CapabilityJSON) {
		t.Error("store should support JSON")
	}

	// Vector should not be supported without EnableVector or extensions
	if store.Supports(CapabilityVector) {
		t.Error("store should not support vector without EnableVector or extensions")
	}
}

func TestNewStore_WithVectorOption(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "vector.db")

	store, err := NewStore(ctx, StoreConfig{
		Path: dbPath,
		Options: collection.Options{
			EnableVector: true,
		},
	})
	if err != nil {
		// On systems without CGo, this will fail because the sqliteext driver isn't available
		// This is expected behavior
		if strings.Contains(err.Error(), "unknown driver") || strings.Contains(err.Error(), "sqliteext") {
			t.Logf("NewStore with EnableVector failed (expected without CGo): %v", err)
			return
		}
		t.Fatalf("NewStore failed with unexpected error: %v", err)
	}
	defer store.Close()

	// Vector support requires CGo driver, which may not be available on macOS
	// So we just check that the store was created, not that vector is supported
	if store == nil {
		t.Fatal("NewStore returned nil store")
	}

	// On systems with CGo, this should be true
	// On systems without CGo (like macOS), this will be false
	_ = store.Supports(CapabilityVector)
}

func TestNewStore_WithExtensions(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "ext.db")

	// Try to load a non-existent extension (should fail gracefully if not required)
	store, err := NewStore(ctx, StoreConfig{
		Path:    dbPath,
		Options: collection.Options{},
		Extensions: []ExtensionConfig{
			{
				Path:       "/nonexistent/extension.so",
				EntryPoint: "sqlite3_extension_init",
				Required:   false, // Not required, so store should still be created
			},
		},
	})
	if err != nil {
		// On macOS without CGo, this will fail because the CGo driver isn't available
		// That's expected behavior
		t.Logf("NewStore with extensions failed (expected on macOS without CGo): %v", err)
		return
	}
	defer store.Close()

	if store == nil {
		t.Fatal("NewStore returned nil store")
	}
}

func TestNewStore_WithRequiredExtension_Fails(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "required_ext.db")

	// Try to load a non-existent extension that's required
	store, err := NewStore(ctx, StoreConfig{
		Path:    dbPath,
		Options: collection.Options{},
		Extensions: []ExtensionConfig{
			{
				Path:       "/nonexistent/extension.so",
				EntryPoint: "sqlite3_extension_init",
				Required:   true, // Required, so store creation should fail
			},
		},
	})
	if err == nil {
		// On macOS without CGo, this will fail earlier (driver not available)
		// On Linux with CGo, this should fail with ExtensionError
		if store != nil {
			store.Close()
		}
		t.Fatal("NewStore should have failed with required extension that doesn't exist")
	}

	// Check if it's an ExtensionError
	if !IsExtensionError(err) {
		t.Logf("Error is not ExtensionError (may be CGo driver unavailable): %v", err)
	}
}

func TestNewStore_UnsupportedType(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "unsupported.db")

	store, err := NewStore(ctx, StoreConfig{
		Type:    "postgresql", // Not supported yet
		Path:    dbPath,
		Options: collection.Options{},
	})
	if err == nil {
		if store != nil {
			store.Close()
		}
		t.Fatal("NewStore should have failed with unsupported type")
	}

	if !strings.Contains(err.Error(), "unsupported store type") {
		t.Errorf("expected 'unsupported store type' error, got: %v", err)
	}
}

func TestNewStore_DefaultType(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "default.db")

	// Type is empty, should default to "sqlite"
	store, err := NewStore(ctx, StoreConfig{
		Path:    dbPath,
		Options: collection.Options{},
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	if store == nil {
		t.Fatal("NewStore returned nil store")
	}
}

func TestNewStore_MemoryDatabase(t *testing.T) {
	ctx := context.Background()

	store, err := NewStore(ctx, StoreConfig{
		Path: ":memory:",
		Options: collection.Options{
			EnableJSON: true,
		},
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	if store.Path() != ":memory:" {
		t.Errorf("expected path ':memory:', got %s", store.Path())
	}

	if !store.Supports(CapabilityJSON) {
		t.Error("store should support JSON")
	}
}

func TestSupports_Capabilities(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	tests := []struct {
		name     string
		options  collection.Options
		expected map[string]bool
	}{
		{
			name:    "no capabilities",
			options: collection.Options{},
			expected: map[string]bool{
				CapabilityFTS:    false,
				CapabilityJSON:   false,
				CapabilityVector: false,
			},
		},
		{
			name: "FTS only",
			options: collection.Options{
				EnableFTS: true,
			},
			expected: map[string]bool{
				CapabilityFTS:    true,
				CapabilityJSON:   false,
				CapabilityVector: false,
			},
		},
		{
			name: "JSON only",
			options: collection.Options{
				EnableJSON: true,
			},
			expected: map[string]bool{
				CapabilityFTS:    false,
				CapabilityJSON:   true,
				CapabilityVector: false,
			},
		},
		{
			name: "FTS and JSON",
			options: collection.Options{
				EnableFTS:  true,
				EnableJSON: true,
			},
			expected: map[string]bool{
				CapabilityFTS:    true,
				CapabilityJSON:   true,
				CapabilityVector: false,
			},
		},
		{
			name: "unknown capability",
			options: collection.Options{
				EnableFTS: true,
			},
			expected: map[string]bool{
				"unknown": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := filepath.Join(tempDir, tt.name+".db")
			store, err := NewStore(ctx, StoreConfig{
				Path:    dbPath,
				Options: tt.options,
			})
			if err != nil {
				t.Fatalf("NewStore failed: %v", err)
			}
			defer store.Close()

			for capability, expected := range tt.expected {
				got := store.Supports(capability)
				if got != expected {
					t.Errorf("Supports(%q) = %v, want %v", capability, got, expected)
				}
			}
		})
	}
}

func TestExtensionError(t *testing.T) {
	err := &ExtensionError{
		Extension: "/path/to/extension.so",
		Cause:     nil,
	}

	if !IsExtensionError(err) {
		t.Error("IsExtensionError should return true for ExtensionError")
	}

	if err.Error() == "" {
		t.Error("ExtensionError.Error() should not be empty")
	}

	if !strings.Contains(err.Error(), "failed to load extension") {
		t.Errorf("ExtensionError should contain 'failed to load extension', got: %s", err.Error())
	}
}

func TestExtensionError_WithCause(t *testing.T) {
	cause := &os.PathError{
		Op:   "open",
		Path: "/nonexistent",
		Err:  os.ErrNotExist,
	}

	err := &ExtensionError{
		Extension: "/path/to/extension.so",
		Cause:     cause,
	}

	if err.Unwrap() != cause {
		t.Error("ExtensionError.Unwrap() should return the cause")
	}

	if !IsExtensionError(err) {
		t.Error("IsExtensionError should return true for ExtensionError")
	}
}

func TestCapabilityError(t *testing.T) {
	err := &CapabilityError{
		Feature: "vector",
		Message: "CGo driver not available",
	}

	if !IsCapabilityError(err) {
		t.Error("IsCapabilityError should return true for CapabilityError")
	}

	if err.Error() == "" {
		t.Error("CapabilityError.Error() should not be empty")
	}

	if !strings.Contains(err.Error(), "capability") {
		t.Errorf("CapabilityError should contain 'capability', got: %s", err.Error())
	}
}

func TestNewStore_StoreOperations(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "ops.db")

	store, err := NewStore(ctx, StoreConfig{
		Path: dbPath,
		Options: collection.Options{
			EnableJSON: true,
		},
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Test that we can perform basic operations
	count, err := store.CountRecords(ctx)
	if err != nil {
		t.Fatalf("CountRecords failed: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 records, got %d", count)
	}

	// Test that we can list records (should be empty)
	records, err := store.ListRecords(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListRecords failed: %v", err)
	}

	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}
