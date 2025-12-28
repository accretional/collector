package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/bootstrap"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
)

func ensureDir(filePath string) error {
	return os.MkdirAll(filepath.Dir(filePath), 0755)
}

// TestTypeValidationIntegration verifies that type validation works end-to-end:
// 1. Bootstrap creates type registry with core types
// 2. CollectionRepo validates message types when creating collections
// 3. Valid types are accepted, invalid types are rejected
func TestTypeValidationIntegration(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	pathConfig := collection.NewPathConfig(tmpDir)

	// Bootstrap system collections (includes type registry with core types)
	systemCollections, err := bootstrap.BootstrapSystemCollections(ctx, pathConfig)
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	defer systemCollections.Close()

	// Create registry store using CollectionRegistryStore (same as production)
	registryStorePath := filepath.Join(tmpDir, "system", "collections.db")
	if err := ensureDir(registryStorePath); err != nil {
		t.Fatalf("Create registry dir failed: %v", err)
	}
	registryDBStore, err := sqlite.NewStore(registryStorePath, collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatalf("Create registry db store failed: %v", err)
	}
	defer registryDBStore.Close()

	registryStore, err := collection.NewCollectionRegistryStoreFromStore(registryDBStore, &collection.LocalFileSystem{})
	if err != nil {
		t.Fatalf("Create registry store failed: %v", err)
	}
	defer registryStore.Close()

	// Create collection repo
	dummyStore, err := sqlite.NewStore(":memory:", collection.Options{})
	if err != nil {
		t.Fatalf("Create dummy store failed: %v", err)
	}
	defer dummyStore.Close()

	storeFactory := func(path string, opts collection.Options) (collection.Store, error) {
		return sqlite.NewStore(path, opts)
	}

	collectionRepo := collection.NewCollectionRepo(dummyStore, pathConfig, registryStore, storeFactory)

	// Wire type validator
	collectionRepo.SetTypeValidator(systemCollections.TypeRegistry)

	// Test 1: Create collection with valid registered type (Collection)
	t.Run("CreateCollection_ValidType_Success", func(t *testing.T) {
		validColl := &pb.Collection{
			Namespace: "test",
			Name:      "valid-collection",
			MessageType: &pb.MessageTypeRef{
				Namespace:   "collector",
				MessageName: "Collection",
			},
		}

		resp, err := collectionRepo.CreateCollection(ctx, validColl)
		if err != nil {
			t.Fatalf("CreateCollection with valid type should succeed: %v", err)
		}

		if resp.CollectionId != "test/valid-collection" {
			t.Errorf("Collection ID = %s, want test/valid-collection", resp.CollectionId)
		}

		t.Logf("✓ Collection created with valid type: %s", resp.CollectionId)
	})

	// Test 2: Create collection with invalid/unregistered type
	t.Run("CreateCollection_InvalidType_Fails", func(t *testing.T) {
		invalidColl := &pb.Collection{
			Namespace: "test",
			Name:      "invalid-collection",
			MessageType: &pb.MessageTypeRef{
				Namespace:   "collector",
				MessageName: "NonExistentMessageType",
			},
		}

		_, err := collectionRepo.CreateCollection(ctx, invalidColl)
		if err == nil {
			t.Fatal("CreateCollection with invalid type should fail")
		}

		if !containsString(err.Error(), "invalid message type") {
			t.Errorf("Error should mention invalid message type, got: %v", err)
		}

		t.Logf("✓ Invalid type correctly rejected: %v", err)
	})

	// Test 3: Create untyped collection (should still work)
	t.Run("CreateCollection_NoType_Success", func(t *testing.T) {
		untypedColl := &pb.Collection{
			Namespace:   "test",
			Name:        "untyped-collection",
			MessageType: nil, // No type specified
		}

		resp, err := collectionRepo.CreateCollection(ctx, untypedColl)
		if err != nil {
			t.Fatalf("CreateCollection without type should succeed: %v", err)
		}

		if resp.CollectionId != "test/untyped-collection" {
			t.Errorf("Collection ID = %s, want test/untyped-collection", resp.CollectionId)
		}

		t.Logf("✓ Untyped collection created successfully: %s", resp.CollectionId)
	})

	// Test 4: Create collection with other valid core types
	t.Run("CreateCollection_OtherCoreTypes_Success", func(t *testing.T) {
		coreTypes := []string{"AuditEvent", "Connection", "RegisteredProto"}

		for _, msgType := range coreTypes {
			coll := &pb.Collection{
				Namespace: "test",
				Name:      "coll-" + msgType,
				MessageType: &pb.MessageTypeRef{
					Namespace:   "collector",
					MessageName: msgType,
				},
			}

			resp, err := collectionRepo.CreateCollection(ctx, coll)
			if err != nil {
				t.Errorf("CreateCollection with %s type failed: %v", msgType, err)
				continue
			}

			t.Logf("✓ Collection created with type %s: %s", msgType, resp.CollectionId)
		}
	})
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
