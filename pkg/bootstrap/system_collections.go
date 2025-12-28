package bootstrap

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SystemCollections holds references to all system collections
type SystemCollections struct {
	// Self-referential collection registry
	CollectionRegistry *collection.Collection

	// Type registry collection (raw)
	TypeRegistryCollection *collection.Collection

	// Type registry with validation logic
	TypeRegistry *TypeRegistry

	// Connection tracking
	Connections *collection.Collection

	// Audit logging
	Audit *collection.Collection

	// System logs (stub)
	Logs *collection.Collection

	pathConfig *collection.PathConfig
}

// BootstrapSystemCollections initializes all system collections in the correct order
// This creates a self-referential bootstrap sequence where:
// 1. Collection registry references itself as first member
// 2. Type registry references all collector types including Collection
// 3. Connection, audit, and logs collections are created and registered
func BootstrapSystemCollections(ctx context.Context, pathConfig *collection.PathConfig) (*SystemCollections, error) {
	log.Println("Starting system collections bootstrap...")

	sc := &SystemCollections{
		pathConfig: pathConfig,
	}

	// Phase 1: Create self-referential collection registry
	if err := sc.bootstrapCollectionRegistry(ctx); err != nil {
		return nil, fmt.Errorf("bootstrap collection registry: %w", err)
	}
	log.Println("✓ Phase 1: Collection registry bootstrapped")

	// Phase 2: Create type registry (stub for now - will add validation interceptor later)
	if err := sc.bootstrapTypeRegistry(ctx); err != nil {
		return nil, fmt.Errorf("bootstrap type registry: %w", err)
	}
	log.Println("✓ Phase 2: Type registry bootstrapped")

	// Phase 3: Create connection collection
	if err := sc.bootstrapConnections(ctx); err != nil {
		return nil, fmt.Errorf("bootstrap connections: %w", err)
	}
	log.Println("✓ Phase 3: Connections collection bootstrapped")

	// Phase 4: Create audit collection
	if err := sc.bootstrapAudit(ctx); err != nil {
		return nil, fmt.Errorf("bootstrap audit: %w", err)
	}
	log.Println("✓ Phase 4: Audit collection bootstrapped")

	// Phase 5: Create logs collection (stub)
	if err := sc.bootstrapLogs(ctx); err != nil {
		return nil, fmt.Errorf("bootstrap logs: %w", err)
	}
	log.Println("✓ Phase 5: Logs collection bootstrapped (stub)")

	log.Println("✓ System collections bootstrap complete")
	return sc, nil
}

// bootstrapCollectionRegistry creates the self-referential collection registry
// This is the "chicken and egg" - the collection that tracks all collections
func (sc *SystemCollections) bootstrapCollectionRegistry(ctx context.Context) error {
	namespace := "system"
	name := "collections"

	// Create database path
	dbPath, err := sc.pathConfig.CollectionDBPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid collection path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	// Create store
	storeOpts := collection.Options{
		EnableFTS:  true,
		EnableJSON: true,
	}
	store, err := sqlite.NewStore(dbPath, storeOpts)
	if err != nil {
		return fmt.Errorf("init sqlite: %w", err)
	}

	// Set JSON converter for Collection type (system type known at compile time)
	store.SetJSONConverter(collection.GetSystemTypeConverter("Collection"))

	// Create filesystem
	filesPath, err := sc.pathConfig.CollectionFilesPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid files path: %w", err)
	}
	fs, err := collection.NewLocalFileSystem(filesPath)
	if err != nil {
		return fmt.Errorf("create filesystem: %w", err)
	}

	// Create collection metadata
	collectionMeta := &pb.Collection{
		Namespace: namespace,
		Name:      name,
		MessageType: &pb.MessageTypeRef{
			Namespace:   "collector",
			MessageName: "Collection",
		},
		IndexedFields: []string{"namespace", "name"},
		Metadata: &pb.Metadata{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
			Labels: map[string]string{
				"system":    "true",
				"bootstrap": "true",
			},
		},
	}

	// Create collection (with bootstrap mode to skip validation)
	coll, err := collection.NewCollectionWithOptions(collectionMeta, store, fs, collection.WithBootstrap(true))
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	sc.CollectionRegistry = coll

	// SELF-REFERENCE: Register itself as the first collection
	collectionBytes, err := proto.Marshal(collectionMeta)
	if err != nil {
		return fmt.Errorf("marshal collection: %w", err)
	}

	// Create labels including both system labels and self-referential marker
	labels := make(map[string]string)
	for k, v := range collectionMeta.Metadata.Labels {
		labels[k] = v
	}
	labels["self-referential"] = "true"

	selfRecord := &pb.CollectionRecord{
		Id:        fmt.Sprintf("%s/%s", namespace, name),
		ProtoData: collectionBytes,
		Metadata: &pb.Metadata{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
			Labels:    labels,
		},
	}

	if err := coll.CreateRecord(ctx, selfRecord); err != nil {
		return fmt.Errorf("create self-reference: %w", err)
	}

	log.Printf("Collection registry self-reference created: %s/%s", namespace, name)
	return nil
}

// bootstrapTypeRegistry creates the type registry collection
// This will store registered proto types and enable request validation
func (sc *SystemCollections) bootstrapTypeRegistry(ctx context.Context) error {
	namespace := "system"
	name := "types"

	// Create database and filesystem
	dbPath, err := sc.pathConfig.CollectionDBPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid collection path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	storeOpts := collection.Options{
		EnableFTS:  true,
		EnableJSON: true,
	}
	store, err := sqlite.NewStore(dbPath, storeOpts)
	if err != nil {
		return fmt.Errorf("init sqlite: %w", err)
	}

	// Set JSON converter for ValidationRule type
	store.SetJSONConverter(collection.GetSystemTypeConverter("ValidationRule"))

	filesPath, err := sc.pathConfig.CollectionFilesPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid files path: %w", err)
	}
	fs, err := collection.NewLocalFileSystem(filesPath)
	if err != nil {
		return fmt.Errorf("create filesystem: %w", err)
	}

	// Create collection metadata
	collectionMeta := &pb.Collection{
		Namespace: namespace,
		Name:      name,
		MessageType: &pb.MessageTypeRef{
			Namespace:   "collector",
			MessageName: "ValidationRule",
		},
		IndexedFields: []string{"namespace", "message_name"},
		Metadata: &pb.Metadata{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
			Labels: map[string]string{
				"system":    "true",
				"bootstrap": "true",
			},
		},
	}

	coll, err := collection.NewCollectionWithOptions(collectionMeta, store, fs, collection.WithBootstrap(true))
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	sc.TypeRegistryCollection = coll

	// Create TypeRegistry wrapper with validation logic
	sc.TypeRegistry = NewTypeRegistry(coll)

	// Register this collection in the collection registry
	if err := sc.registerCollection(ctx, collectionMeta); err != nil {
		return fmt.Errorf("register type collection: %w", err)
	}

	// Register core collector types
	if err := sc.registerCoreTypes(ctx); err != nil {
		return fmt.Errorf("register core types: %w", err)
	}

	log.Println("✓ Core collector types registered")

	return nil
}

// bootstrapConnections creates the connection tracking collection
func (sc *SystemCollections) bootstrapConnections(ctx context.Context) error {
	namespace := "system"
	name := "connections"

	dbPath, err := sc.pathConfig.CollectionDBPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid collection path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	storeOpts := collection.Options{
		EnableFTS:  true,
		EnableJSON: true,
	}
	store, err := sqlite.NewStore(dbPath, storeOpts)
	if err != nil {
		return fmt.Errorf("init sqlite: %w", err)
	}

	// Set JSON converter for Connection type
	store.SetJSONConverter(collection.GetSystemTypeConverter("Connection"))

	filesPath, err := sc.pathConfig.CollectionFilesPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid files path: %w", err)
	}
	fs, err := collection.NewLocalFileSystem(filesPath)
	if err != nil {
		return fmt.Errorf("create filesystem: %w", err)
	}

	collectionMeta := &pb.Collection{
		Namespace: namespace,
		Name:      name,
		MessageType: &pb.MessageTypeRef{
			Namespace:   "collector",
			MessageName: "Connection",
		},
		IndexedFields: []string{"source_collector_id", "target_collector_id", "address"},
		Metadata: &pb.Metadata{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
			Labels: map[string]string{
				"system":    "true",
				"bootstrap": "true",
			},
		},
	}

	coll, err := collection.NewCollectionWithOptions(collectionMeta, store, fs, collection.WithBootstrap(true))
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	sc.Connections = coll

	// Register in collection registry
	if err := sc.registerCollection(ctx, collectionMeta); err != nil {
		return fmt.Errorf("register connections collection: %w", err)
	}

	// TODO: Migrate existing in-memory connections to this collection
	// TODO: Add in-memory cache for performance-critical connection lookups
	// TODO: Update pkg/dispatch/connection_manager.go to use this collection

	return nil
}

// bootstrapAudit creates the audit logging collection
func (sc *SystemCollections) bootstrapAudit(ctx context.Context) error {
	namespace := "system"
	name := "audit"

	dbPath, err := sc.pathConfig.CollectionDBPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid collection path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	storeOpts := collection.Options{
		EnableFTS:  true,
		EnableJSON: true,
	}
	store, err := sqlite.NewStore(dbPath, storeOpts)
	if err != nil {
		return fmt.Errorf("init sqlite: %w", err)
	}

	// Set JSON converter for AuditEvent type
	store.SetJSONConverter(collection.GetSystemTypeConverter("AuditEvent"))

	filesPath, err := sc.pathConfig.CollectionFilesPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid files path: %w", err)
	}
	fs, err := collection.NewLocalFileSystem(filesPath)
	if err != nil {
		return fmt.Errorf("create filesystem: %w", err)
	}

	collectionMeta := &pb.Collection{
		Namespace: namespace,
		Name:      name,
		MessageType: &pb.MessageTypeRef{
			Namespace:   "collector",
			MessageName: "AuditEvent",
		},
		IndexedFields: []string{"namespace", "collection_name", "operation", "actor_id", "timestamp"},
		Metadata: &pb.Metadata{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
			Labels: map[string]string{
				"system":    "true",
				"bootstrap": "true",
			},
		},
	}

	coll, err := collection.NewCollectionWithOptions(collectionMeta, store, fs, collection.WithBootstrap(true))
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	sc.Audit = coll

	// Register in collection registry
	if err := sc.registerCollection(ctx, collectionMeta); err != nil {
		return fmt.Errorf("register audit collection: %w", err)
	}

	// TODO: Add buffered audit logger that batches inserts for performance
	// TODO: Add audit interceptor to all gRPC services
	// TODO: Define audit policy (retention, what to log, etc.)
	// TODO: Add audit query helpers (search by namespace, operation, time range, etc.)

	return nil
}

// bootstrapLogs creates the system logs collection for structured logging
func (sc *SystemCollections) bootstrapLogs(ctx context.Context) error {
	namespace := "system"
	name := "logs"

	dbPath, err := sc.pathConfig.CollectionDBPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid collection path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	storeOpts := collection.Options{
		EnableFTS:  true,
		EnableJSON: true,
	}
	store, err := sqlite.NewStore(dbPath, storeOpts)
	if err != nil {
		return fmt.Errorf("init sqlite: %w", err)
	}

	// Set JSON converter for SystemLog type
	store.SetJSONConverter(collection.GetSystemTypeConverter("SystemLog"))

	filesPath, err := sc.pathConfig.CollectionFilesPath(namespace, name)
	if err != nil {
		return fmt.Errorf("invalid files path: %w", err)
	}
	fs, err := collection.NewLocalFileSystem(filesPath)
	if err != nil {
		return fmt.Errorf("create filesystem: %w", err)
	}

	collectionMeta := &pb.Collection{
		Namespace: namespace,
		Name:      name,
		MessageType: &pb.MessageTypeRef{
			Namespace:   "collector",
			MessageName: "SystemLog",
		},
		IndexedFields: []string{"level", "component", "namespace", "timestamp"},
		Metadata: &pb.Metadata{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
			Labels: map[string]string{
				"system":    "true",
				"bootstrap": "true",
			},
		},
	}

	coll, err := collection.NewCollectionWithOptions(collectionMeta, store, fs, collection.WithBootstrap(true))
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	sc.Logs = coll

	// Register in collection registry
	if err := sc.registerCollection(ctx, collectionMeta); err != nil {
		return fmt.Errorf("register logs collection: %w", err)
	}

	// Future enhancements:
	// - Add log rotation policy (time-based and size-based)
	// - Add log forwarding to external systems (e.g., Loki, Elasticsearch)
	// - Add sampling for high-frequency log entries
	// - Add alerting based on log patterns (e.g., error rate threshold)

	return nil
}

// registerCollection adds a collection to the collection registry
func (sc *SystemCollections) registerCollection(ctx context.Context, collectionMeta *pb.Collection) error {
	if sc.CollectionRegistry == nil {
		return fmt.Errorf("collection registry not initialized")
	}

	collectionBytes, err := proto.Marshal(collectionMeta)
	if err != nil {
		return fmt.Errorf("marshal collection: %w", err)
	}

	record := &pb.CollectionRecord{
		Id:        fmt.Sprintf("%s/%s", collectionMeta.Namespace, collectionMeta.Name),
		ProtoData: collectionBytes,
		Metadata: &pb.Metadata{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
			Labels:    collectionMeta.Metadata.Labels, // Preserve labels from collection metadata
		},
	}

	if err := sc.CollectionRegistry.CreateRecord(ctx, record); err != nil {
		return fmt.Errorf("create collection record: %w", err)
	}

	return nil
}

// registerCoreTypes registers the core collector message types
func (sc *SystemCollections) registerCoreTypes(ctx context.Context) error {
	// Get the file descriptors for collector proto messages
	// These are the core types that all collections use

	protoFiles := []struct {
		name string
		file protoreflect.FileDescriptor
	}{
		{"collection.proto", pb.File_collection_proto},
		{"common.proto", pb.File_common_proto},
		{"system.proto", pb.File_system_proto},
		{"registry.proto", pb.File_registry_proto},
		{"dispatcher.proto", pb.File_dispatcher_proto},
		{"collection_repo.proto", pb.File_collection_repo_proto},
	}

	for _, pf := range protoFiles {
		// Convert FileDescriptor to FileDescriptorProto
		fileDescProto := protodesc.ToFileDescriptorProto(pf.file)

		if err := sc.TypeRegistry.RegisterFileDescriptor(ctx, "collector", fileDescProto); err != nil {
			return fmt.Errorf("register %s types: %w", pf.name, err)
		}
	}

	log.Printf("Registered core collector types from %d proto files", len(protoFiles))
	return nil
}

// Close closes all system collections
func (sc *SystemCollections) Close() error {
	var lastErr error

	if sc.Logs != nil {
		if err := sc.Logs.Close(); err != nil {
			lastErr = err
			log.Printf("Error closing logs collection: %v", err)
		}
	}

	if sc.Audit != nil {
		if err := sc.Audit.Close(); err != nil {
			lastErr = err
			log.Printf("Error closing audit collection: %v", err)
		}
	}

	if sc.Connections != nil {
		if err := sc.Connections.Close(); err != nil {
			lastErr = err
			log.Printf("Error closing connections collection: %v", err)
		}
	}

	if sc.TypeRegistryCollection != nil {
		if err := sc.TypeRegistryCollection.Close(); err != nil {
			lastErr = err
			log.Printf("Error closing type registry: %v", err)
		}
	}

	if sc.CollectionRegistry != nil {
		if err := sc.CollectionRegistry.Close(); err != nil {
			lastErr = err
			log.Printf("Error closing collection registry: %v", err)
		}
	}

	return lastErr
}
