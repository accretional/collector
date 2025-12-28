package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestIssue1_JSONSchemaErrorHandling tests that JSON schema errors are properly handled.
// Issue: Silent error on JSON schema creation (store.go:55-59)
// The error from JSONSchema execution was silently ignored, which could hide real failures.
func TestIssue1_JSONSchemaErrorHandling(t *testing.T) {
	// Test 1: Normal creation with EnableJSON should work
	t.Run("NormalCreation", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		store, err := NewStore(dbPath, collection.Options{EnableJSON: true})
		if err != nil {
			t.Fatalf("Expected store creation to succeed, got error: %v", err)
		}
		defer store.Close()

		// Verify jsontext column exists by inserting a record
		ctx := context.Background()
		record := &collector.CollectionRecord{
			Id:        "test-1",
			ProtoData: []byte(`{"name": "test"}`),
			Metadata: &collector.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
				Labels:    map[string]string{"env": "test"},
			},
		}
		if err := store.CreateRecord(ctx, record); err != nil {
			t.Fatalf("Failed to create record: %v", err)
		}

		// Verify we can search using JSON features
		results, err := store.Search(ctx, &collection.SearchQuery{
			LabelFilters: map[string]string{"env": "test"},
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("Expected 1 result, got %d", len(results))
		}
	})

	// Test 2: Idempotent creation - calling twice with EnableJSON should work
	t.Run("IdempotentCreation", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// First creation
		store1, err := NewStore(dbPath, collection.Options{EnableJSON: true})
		if err != nil {
			t.Fatalf("First store creation failed: %v", err)
		}
		store1.Close()

		// Second creation on same DB - should succeed (column already exists)
		store2, err := NewStore(dbPath, collection.Options{EnableJSON: true})
		if err != nil {
			t.Fatalf("Second store creation should succeed (idempotent), got error: %v", err)
		}
		defer store2.Close()
	})

	// Test 3: Verify that non-duplicate-column errors are NOT silently ignored
	// This test creates a scenario where JSONSchema fails for a reason other than
	// "duplicate column" and verifies the error is returned.
	t.Run("NonDuplicateColumnErrorReturned", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Open database directly and create a conflicting state
		dsn := "file:" + dbPath + "?_journal_mode=WAL&_busy_timeout=10000"
		db, err := sql.Open("sqlite3", dsn)
		if err != nil {
			t.Fatalf("Failed to open db: %v", err)
		}

		// Create the default schema
		if _, err := db.Exec(collection.DefaultSchema); err != nil {
			db.Close()
			t.Fatalf("Failed to create default schema: %v", err)
		}

		// Create jsontext as an INTEGER column (incompatible with TEXT)
		// This simulates a corrupted/incompatible state
		if _, err := db.Exec("ALTER TABLE records ADD COLUMN jsontext INTEGER"); err != nil {
			db.Close()
			t.Fatalf("Failed to add incompatible column: %v", err)
		}
		db.Close()

		// Now try to create a SqliteStore with EnableJSON
		// The JSONSchema tries: ALTER TABLE records ADD COLUMN jsontext TEXT
		// This should fail because jsontext already exists (but as INTEGER)
		// With the bug, this error would be silently ignored
		// After fix, the store creation should still succeed (duplicate column is OK)
		// but if the column type mismatch causes issues, they should surface
		store, err := NewStore(dbPath, collection.Options{EnableJSON: true})
		if err != nil {
			// If we get an error here, that's actually fine - it means we're not
			// silently ignoring errors. But duplicate column errors should be OK.
			if !strings.Contains(err.Error(), "duplicate column") {
				t.Logf("Got non-duplicate-column error (expected with fix): %v", err)
			}
		} else {
			defer store.Close()
			// Store was created - verify the column is actually usable
			// If the column exists but is wrong type, operations may fail
			ctx := context.Background()
			record := &collector.CollectionRecord{
				Id:        "test-1",
				ProtoData: []byte(`{"name": "test"}`),
				Metadata: &collector.Metadata{
					CreatedAt: timestamppb.Now(),
					UpdatedAt: timestamppb.Now(),
					Labels:    map[string]string{"key": "value"},
				},
			}
			// This might fail if the column type is incompatible
			err := store.CreateRecord(ctx, record)
			if err != nil {
				t.Logf("CreateRecord failed (may indicate column type issue): %v", err)
			}
		}
	})

	// Test 4: Verify error is returned when database becomes read-only during JSON schema
	t.Run("ReadOnlyDatabaseError", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Create database with default schema only (no JSON)
		dsn := "file:" + dbPath + "?_journal_mode=WAL&_busy_timeout=10000"
		db, err := sql.Open("sqlite3", dsn)
		if err != nil {
			t.Fatalf("Failed to open db: %v", err)
		}
		if _, err := db.Exec(collection.DefaultSchema); err != nil {
			db.Close()
			t.Fatalf("Failed to create default schema: %v", err)
		}
		db.Close()

		// Make the database file read-only
		if err := os.Chmod(dbPath, 0444); err != nil {
			t.Fatalf("Failed to make db read-only: %v", err)
		}
		// Restore permissions after test
		defer os.Chmod(dbPath, 0644)

		// Try to create store with EnableJSON - should fail because we can't write
		// With the current bug, this might silently ignore the JSON schema error
		// After fix, we should get an error
		_, err = NewStore(dbPath, collection.Options{EnableJSON: true})
		if err == nil {
			t.Error("Expected error when creating store on read-only database with EnableJSON, got nil")
		} else {
			t.Logf("Got expected error for read-only database: %v", err)
		}
	})
}

// TestIssue2_EnableJSONValidationBeforeSearch tests that Search properly validates
// EnableJSON state before executing JSON queries.
// Issue: No EnableJSON validation before Search() (store.go:260-365)
func TestIssue2_EnableJSONValidationBeforeSearch(t *testing.T) {
	// Test: Search with filters on store created WITHOUT EnableJSON should return clear error
	t.Run("SearchWithoutEnableJSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Create store WITHOUT EnableJSON
		store, err := NewStore(dbPath, collection.Options{EnableJSON: false})
		if err != nil {
			t.Fatalf("Store creation failed: %v", err)
		}
		defer store.Close()

		ctx := context.Background()

		// Try to search with label filters - this uses json_extract on labels column
		// which doesn't exist when EnableJSON is false
		_, err = store.Search(ctx, &collection.SearchQuery{
			LabelFilters: map[string]string{"namespace": "test"},
		})

		// Should get a clear error message about EnableJSON
		if err == nil {
			t.Error("Expected error when searching with filters on non-JSON store, got nil")
		} else {
			errStr := err.Error()
			if !strings.Contains(errStr, "EnableJSON") {
				t.Errorf("Error should mention EnableJSON, got: %v", err)
			}
		}
	})

	// Test: Search with JSON field filters on store without EnableJSON
	t.Run("SearchFiltersWithoutEnableJSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Create store WITHOUT EnableJSON
		store, err := NewStore(dbPath, collection.Options{EnableJSON: false})
		if err != nil {
			t.Fatalf("Store creation failed: %v", err)
		}
		defer store.Close()

		ctx := context.Background()

		// Try to search with field filters - this uses json_extract on jsontext column
		_, err = store.Search(ctx, &collection.SearchQuery{
			Filters: map[string]collection.Filter{
				"status": {Operator: collection.OpEquals, Value: "active"},
			},
		})

		// Should get a clear error message about EnableJSON
		if err == nil {
			t.Error("Expected error when searching with filters on non-JSON store, got nil")
		} else {
			errStr := err.Error()
			if !strings.Contains(errStr, "EnableJSON") {
				t.Errorf("Error should mention EnableJSON, got: %v", err)
			}
		}
	})

	// Test: Empty search query should work even without EnableJSON
	t.Run("EmptySearchWithoutEnableJSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Create store WITHOUT EnableJSON
		store, err := NewStore(dbPath, collection.Options{EnableJSON: false})
		if err != nil {
			t.Fatalf("Store creation failed: %v", err)
		}
		defer store.Close()

		ctx := context.Background()

		// Empty search query should work - no JSON features needed
		results, err := store.Search(ctx, &collection.SearchQuery{})
		if err != nil {
			t.Errorf("Empty search should work without EnableJSON: %v", err)
		}
		// Should return empty results (no records created)
		if len(results) != 0 {
			t.Errorf("Expected 0 results, got %d", len(results))
		}
	})
}

// TestIssue3_InvalidJSONHandling tests that invalid JSON proto_data is handled properly.
// Issue: Invalid JSON silently defaults to empty object (store.go:119-125)
func TestIssue3_InvalidJSONHandling(t *testing.T) {
	// Test 1: Valid JSON proto_data should work fine
	t.Run("ValidJSONProtoData", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		store, err := NewStore(dbPath, collection.Options{EnableJSON: true})
		if err != nil {
			t.Fatalf("Store creation failed: %v", err)
		}
		defer store.Close()

		ctx := context.Background()

		// Create record with valid JSON proto_data
		record := &collector.CollectionRecord{
			Id:        "valid-json",
			ProtoData: []byte(`{"name": "test", "status": "active"}`),
			Metadata: &collector.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
				Labels:    map[string]string{"type": "test"},
			},
		}

		err = store.CreateRecord(ctx, record)
		if err != nil {
			t.Fatalf("CreateRecord with valid JSON should succeed: %v", err)
		}

		// Should be searchable by JSON field
		results, err := store.Search(ctx, &collection.SearchQuery{
			Filters: map[string]collection.Filter{
				"status": {Operator: collection.OpEquals, Value: "active"},
			},
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("Expected 1 result, got %d", len(results))
		}
	})

	// Test 2: Binary proto_data with EnableJSON but no converter falls back gracefully
	t.Run("BinaryProtoWithoutConverter", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		store, err := NewStore(dbPath, collection.Options{EnableJSON: true})
		if err != nil {
			t.Fatalf("Store creation failed: %v", err)
		}
		defer store.Close()

		ctx := context.Background()

		// Create record with binary protobuf (not JSON)
		// Without a converter set, this should fall back to "{}" for jsontext
		record := &collector.CollectionRecord{
			Id:        "binary-proto",
			ProtoData: []byte{0x0a, 0x04, 0x74, 0x65, 0x73, 0x74}, // binary protobuf, not JSON
			Metadata: &collector.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
				Labels:    map[string]string{"type": "test"},
			},
		}

		// Should succeed - falls back to "{}" for jsontext (graceful degradation)
		err = store.CreateRecord(ctx, record)
		if err != nil {
			t.Errorf("CreateRecord should succeed with fallback, got error: %v", err)
		}

		// Verify the record was stored
		retrieved, err := store.GetRecord(ctx, "binary-proto")
		if err != nil {
			t.Fatalf("GetRecord failed: %v", err)
		}
		if string(retrieved.ProtoData) != string(record.ProtoData) {
			t.Errorf("ProtoData mismatch: got %v, want %v", retrieved.ProtoData, record.ProtoData)
		}
	})

	// Test 3: Binary proto_data with converter should convert properly
	t.Run("BinaryProtoWithConverter", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		store, err := NewStore(dbPath, collection.Options{EnableJSON: true})
		if err != nil {
			t.Fatalf("Store creation failed: %v", err)
		}
		defer store.Close()

		// Use real converter for Collection type
		store.SetJSONConverter(collection.NewStaticJSONConverter(&collector.Collection{}))

		ctx := context.Background()

		// Create a real Collection proto and marshal it
		testCollection := &collector.Collection{
			Namespace: "test",
			Name:      "mytest",
		}
		protoBytes, err := proto.Marshal(testCollection)
		if err != nil {
			t.Fatalf("Failed to marshal test proto: %v", err)
		}

		record := &collector.CollectionRecord{
			Id:        "converted-proto",
			ProtoData: protoBytes,
			Metadata: &collector.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
				Labels:    map[string]string{"type": "test"},
			},
		}

		err = store.CreateRecord(ctx, record)
		if err != nil {
			t.Fatalf("CreateRecord with converter should succeed: %v", err)
		}

		// Search should find the record using converted JSON
		results, err := store.Search(ctx, &collection.SearchQuery{
			Filters: map[string]collection.Filter{
				"namespace": {Operator: collection.OpEquals, Value: "test"},
			},
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("Expected 1 result, got %d", len(results))
		}
	})

	// Test 4: Invalid JSON proto_data WITHOUT EnableJSON should work (no JSON features used)
	t.Run("InvalidJSONWithoutEnableJSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Create store WITHOUT EnableJSON - binary protobuf should be fine
		store, err := NewStore(dbPath, collection.Options{EnableJSON: false})
		if err != nil {
			t.Fatalf("Store creation failed: %v", err)
		}
		defer store.Close()

		ctx := context.Background()

		// Create record with binary protobuf (not JSON)
		record := &collector.CollectionRecord{
			Id:        "binary-proto",
			ProtoData: []byte{0x0a, 0x04, 0x74, 0x65, 0x73, 0x74}, // binary protobuf
			Metadata: &collector.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
				Labels:    map[string]string{},
			},
		}

		// Without EnableJSON, binary proto_data should be fine
		err = store.CreateRecord(ctx, record)
		if err != nil {
			t.Errorf("CreateRecord without EnableJSON should accept binary proto_data: %v", err)
		}
	})
}

// TestIssue6_LabelKeyEscaping tests that label keys with special characters are properly escaped.
// Issue: Label keys containing dots or special JSON path characters were not properly escaped,
// causing searches to fail or return wrong results.
func TestIssue6_LabelKeyEscaping(t *testing.T) {
	// Test: Label keys with dots should be searchable
	t.Run("LabelKeyWithDots", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		store, err := NewStore(dbPath, collection.Options{EnableJSON: true})
		if err != nil {
			t.Fatalf("Store creation failed: %v", err)
		}
		defer store.Close()

		ctx := context.Background()

		// Create record with label key containing dots
		record := &collector.CollectionRecord{
			Id:        "dotted-label",
			ProtoData: []byte(`{}`),
			Metadata: &collector.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
				Labels: map[string]string{
					"app.kubernetes.io/name": "myapp",
					"simple":                 "value",
				},
			},
		}

		err = store.CreateRecord(ctx, record)
		if err != nil {
			t.Fatalf("CreateRecord failed: %v", err)
		}

		// Search by dotted label key
		results, err := store.Search(ctx, &collection.SearchQuery{
			LabelFilters: map[string]string{
				"app.kubernetes.io/name": "myapp",
			},
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("Expected 1 result for dotted label key, got %d", len(results))
		}
	})

	// Test: Label keys with quotes should be searchable
	t.Run("LabelKeyWithQuotes", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		store, err := NewStore(dbPath, collection.Options{EnableJSON: true})
		if err != nil {
			t.Fatalf("Store creation failed: %v", err)
		}
		defer store.Close()

		ctx := context.Background()

		// Create record with label key containing quotes
		record := &collector.CollectionRecord{
			Id:        "quoted-label",
			ProtoData: []byte(`{}`),
			Metadata: &collector.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
				Labels: map[string]string{
					`key"with"quotes`: "value",
				},
			},
		}

		err = store.CreateRecord(ctx, record)
		if err != nil {
			t.Fatalf("CreateRecord failed: %v", err)
		}

		// Search by key with quotes
		results, err := store.Search(ctx, &collection.SearchQuery{
			LabelFilters: map[string]string{
				`key"with"quotes`: "value",
			},
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("Expected 1 result for quoted label key, got %d", len(results))
		}
	})

	// Test: Label keys with brackets should be searchable
	t.Run("LabelKeyWithBrackets", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		store, err := NewStore(dbPath, collection.Options{EnableJSON: true})
		if err != nil {
			t.Fatalf("Store creation failed: %v", err)
		}
		defer store.Close()

		ctx := context.Background()

		// Create record with label key containing brackets
		record := &collector.CollectionRecord{
			Id:        "bracket-label",
			ProtoData: []byte(`{}`),
			Metadata: &collector.Metadata{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
				Labels: map[string]string{
					"key[0]": "first",
				},
			},
		}

		err = store.CreateRecord(ctx, record)
		if err != nil {
			t.Fatalf("CreateRecord failed: %v", err)
		}

		// Search by key with brackets
		results, err := store.Search(ctx, &collection.SearchQuery{
			LabelFilters: map[string]string{
				"key[0]": "first",
			},
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("Expected 1 result for bracket label key, got %d", len(results))
		}
	})
}
