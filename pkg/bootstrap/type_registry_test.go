package bootstrap

import (
	"context"
	"path/filepath"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestTypeRegistry_CacheInvalidation(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// 1. Setup minimal collection for types
	dbPath := filepath.Join(tempDir, "types.db")
	store, err := sqlite.NewStore(dbPath, collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	coll, err := collection.NewCollection(
		&pb.Collection{Namespace: "system", Name: "types"},
		store,
		&collection.LocalFileSystem{},
	)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	tr := NewTypeRegistry(coll)

	// 2. Register a Type (Initial Write)
	namespace := "test"
	msgName := "CacheTestMsg"
	fileDesc := &descriptorpb.FileDescriptorProto{Name: proto.String("test.proto")}
	msgDesc := &descriptorpb.DescriptorProto{
		Name: proto.String(msgName),
		Field: []*descriptorpb.FieldDescriptorProto{
			{Name: proto.String("field_one"), Number: proto.Int32(1)},
		},
	}

	if err := tr.RegisterMessageType(ctx, namespace, msgDesc, fileDesc); err != nil {
		t.Fatalf("RegisterMessageType failed: %v", err)
	}

	// 3. Verify Cache Hit (Read)
	// We can't access tr.cache directly (private), but we can verify behavior
	// or use reflection/export for whitebox testing.
	// Since we are in the same package (bootstrap), we CAN access private fields if we use package bootstrap.

	typeID := namespace + "/" + msgName
	tr.mu.RLock()
	cachedRule, inCache := tr.cache[typeID]
	tr.mu.RUnlock()

	if !inCache {
		t.Error("RegisterMessageType did not populate cache")
	}
	if len(cachedRule.FieldRules) != 1 || cachedRule.FieldRules[0].FieldName != "field_one" {
		t.Errorf("Cached rule has wrong fields: %+v", cachedRule)
	}

	// 4. Update the Type (Write Update)
	// Add a second field
	msgDescV2 := &descriptorpb.DescriptorProto{
		Name: proto.String(msgName),
		Field: []*descriptorpb.FieldDescriptorProto{
			{Name: proto.String("field_one"), Number: proto.Int32(1)},
			{Name: proto.String("field_two"), Number: proto.Int32(2)},
		},
	}

	if err := tr.RegisterMessageType(ctx, namespace, msgDescV2, fileDesc); err != nil {
		t.Fatalf("RegisterMessageType (Update) failed: %v", err)
	}

	// 5. Verify Cache Update
	tr.mu.RLock()
	cachedRuleV2, inCacheV2 := tr.cache[typeID]
	tr.mu.RUnlock()

	if !inCacheV2 {
		t.Error("Update removed item from cache?")
	}
	if len(cachedRuleV2.FieldRules) != 2 {
		t.Errorf("Cache was not updated! Expected 2 fields, got %d", len(cachedRuleV2.FieldRules))
	}

	// 6. Verify GetMessageType uses the new data
	rule, err := tr.GetMessageType(ctx, namespace, msgName)
	if err != nil {
		t.Fatalf("GetMessageType failed: %v", err)
	}
	if len(rule.FieldRules) != 2 {
		t.Errorf("GetMessageType returned stale data")
	}
}
