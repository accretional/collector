package collection

import (
	"context"
	"fmt"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CollectionRegistryStore implements RegistryStore using a Collection.
// This allows the registry to be self-hosted in the "system/collections" collection.
type CollectionRegistryStore struct {
	collection *Collection
}

// NewCollectionRegistryStore creates a new registry store backed by a collection.
func NewCollectionRegistryStore(collection *Collection) *CollectionRegistryStore {
	return &CollectionRegistryStore{
		collection: collection,
	}
}

// SaveCollection persists a collection to the registry.
func (s *CollectionRegistryStore) SaveCollection(ctx context.Context, collection *pb.Collection, dbPath string) error {
	id := fmt.Sprintf("%s/%s", collection.Namespace, collection.Name)

	// Clone collection to avoid modifying the input
	collCopy := proto.Clone(collection).(*pb.Collection)

	// Ensure metadata exists
	if collCopy.Metadata == nil {
		collCopy.Metadata = &pb.Metadata{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
			Labels:    make(map[string]string),
		}
	} else {
		collCopy.Metadata.UpdatedAt = timestamppb.Now()
		if collCopy.Metadata.CreatedAt == nil {
			collCopy.Metadata.CreatedAt = timestamppb.Now()
		}
		if collCopy.Metadata.Labels == nil {
			collCopy.Metadata.Labels = make(map[string]string)
		}
	}

	// Store dbPath in labels for retrieval
	if dbPath != "" {
		collCopy.Metadata.Labels["db_path"] = dbPath
	}

	// Marshal the collection proto
	protoData, err := proto.Marshal(collCopy)
	if err != nil {
		return fmt.Errorf("marshal collection: %w", err)
	}

	// Create record
	record := &pb.CollectionRecord{
		Id:        id,
		ProtoData: protoData,
		Metadata:  collCopy.Metadata,
	}

	// Check if it exists first to decide between Create and Update
	// TODO: Collection.CreateRecord should probably support upsert or we should add PutRecord
	_, err = s.collection.GetRecord(ctx, id)
	if err == nil {
		return s.collection.UpdateRecord(ctx, record)
	}
	return s.collection.CreateRecord(ctx, record)
}

// GetCollection retrieves a collection by namespace and name.
func (s *CollectionRegistryStore) GetCollection(ctx context.Context, namespace, name string) (*CollectionMetadata, error) {
	id := fmt.Sprintf("%s/%s", namespace, name)

	record, err := s.collection.GetRecord(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get collection record: %w", err)
	}

	var coll pb.Collection
	if err := proto.Unmarshal(record.ProtoData, &coll); err != nil {
		return nil, fmt.Errorf("unmarshal collection: %w", err)
	}

	// Reconstruct dbPath (it's not stored directly in the proto, but derived)
	// In the SqliteRegistryStore it was stored, but here we might need to rely on PathConfig conventions
	// or store it in the Metadata labels if strictly necessary.
	// However, for now, let's assume standard paths or that the caller (Repo) handles paths.
	// Wait, RegistryStore interface returns DBPath.
	// The SqliteStore stored it explicitly.
	// We should store it in the collection proto, maybe in Metadata?
	// Or just return the standard path based on namespace/name?
	// The interface signature requires returning it.

	// Check if we stored it in labels?
	// Let's modify SaveCollection to store dbPath in labels if needed,
	// but standardizing on PathConfig is better.
	// For backward compatibility with the interface, we'll try to look it up from labels
	// or return a default if not found.

	dbPath := ""
	if coll.Metadata != nil && coll.Metadata.Labels != nil {
		dbPath = coll.Metadata.Labels["db_path"]
	}

	// If not in labels, the caller might reconstruct it using PathConfig.
	// But let's verify what SqliteRegistryStore did.
	// It stored 'db_path' column.

	return &CollectionMetadata{
		Collection: &coll,
		DBPath:     dbPath,
	}, nil
}

// ListCollections returns all collections, optionally filtered by namespace.
func (s *CollectionRegistryStore) ListCollections(ctx context.Context, namespace string) ([]*CollectionMetadata, error) {
	var query *SearchQuery

	if namespace != "" {
		query = &SearchQuery{
			Filters: map[string]Filter{
				"namespace": {Operator: OpEquals, Value: namespace},
			},
		}
	} else {
		query = &SearchQuery{} // List all
	}

	// Search returns SearchResults wrapping Records
	results, err := s.collection.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search collections: %w", err)
	}

	var metadataList []*CollectionMetadata
	for _, result := range results {
		var coll pb.Collection
		if err := proto.Unmarshal(result.Record.ProtoData, &coll); err != nil {
			// Log error but continue?
			continue
		}

		dbPath := ""
		if coll.Metadata != nil && coll.Metadata.Labels != nil {
			dbPath = coll.Metadata.Labels["db_path"]
		}

		metadataList = append(metadataList, &CollectionMetadata{
			Collection: &coll,
			DBPath:     dbPath,
		})
	}

	return metadataList, nil
}

// DeleteCollection removes a collection from the registry.
func (s *CollectionRegistryStore) DeleteCollection(ctx context.Context, namespace, name string) error {
	id := fmt.Sprintf("%s/%s", namespace, name)
	return s.collection.DeleteRecord(ctx, id)
}

// UpdateMetadata updates the metadata for an existing collection.
func (s *CollectionRegistryStore) UpdateMetadata(ctx context.Context, namespace, name string, meta *pb.Collection) error {
	// Reuse SaveCollection which handles upsert logic essentially
	// We need the dbPath though.

	// First get existing to preserve DBPath if it's there
	existing, err := s.GetCollection(ctx, namespace, name)
	if err != nil {
		return err
	}

	return s.SaveCollection(ctx, meta, existing.DBPath)
}

// Close closes the underlying collection (if needed).
func (s *CollectionRegistryStore) Close() error {
	// The collection itself is managed by the system, we don't necessarily close it here
	// unless we own it. But usually stores are closed by the repo/server.
	return nil
}
