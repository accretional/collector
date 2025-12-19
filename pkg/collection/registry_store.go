package collection

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	_ "github.com/mattn/go-sqlite3"
)

// RegistrySchema defines the SQL schema for the collection registry.
const RegistrySchema = `
CREATE TABLE IF NOT EXISTS collections (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    db_path TEXT NOT NULL,
    message_type_name TEXT,
    server_endpoint TEXT,
    indexed_fields TEXT,
    labels TEXT,
    backup_policy TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(namespace, name)
);
CREATE INDEX IF NOT EXISTS idx_namespace ON collections(namespace);
CREATE INDEX IF NOT EXISTS idx_labels ON collections(labels);
`

// CollectionMetadata holds collection information with its storage path.
type CollectionMetadata struct {
	Collection *pb.Collection
	DBPath     string // Relative path: "{namespace}/{name}.db"
}

// RegistryStore defines the interface for persisting collection metadata.
type RegistryStore interface {
	SaveCollection(ctx context.Context, collection *pb.Collection, dbPath string) error
	GetCollection(ctx context.Context, namespace, name string) (*CollectionMetadata, error)
	ListCollections(ctx context.Context, namespace string) ([]*CollectionMetadata, error)
	DeleteCollection(ctx context.Context, namespace, name string) error
	UpdateMetadata(ctx context.Context, namespace, name string, meta *pb.Collection) error
	Close() error
}

// SqliteRegistryStore implements RegistryStore using SQLite.
type SqliteRegistryStore struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// NewSqliteRegistryStore creates a new SQLite-backed registry store.
func NewSqliteRegistryStore(path string) (*SqliteRegistryStore, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open registry database: %w", err)
	}

	// Apply schema
	if _, err := db.Exec(RegistrySchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to apply schema: %w", err)
	}

	return &SqliteRegistryStore{
		db:   db,
		path: path,
	}, nil
}

// SaveCollection persists a collection to the registry.
func (s *SqliteRegistryStore) SaveCollection(ctx context.Context, collection *pb.Collection, dbPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("%s/%s", collection.Namespace, collection.Name)
	now := time.Now().Unix()

	// Encode indexed fields, labels, and backup policy as JSON
	var indexedFieldsJSON, labelsJSON, backupPolicyJSON []byte
	var err error

	if collection.IndexedFields != nil && len(collection.IndexedFields) > 0 {
		indexedFieldsJSON, err = json.Marshal(collection.IndexedFields)
		if err != nil {
			return fmt.Errorf("failed to marshal indexed fields: %w", err)
		}
	}

	if collection.Metadata != nil && collection.Metadata.Labels != nil {
		labelsJSON, err = json.Marshal(collection.Metadata.Labels)
		if err != nil {
			return fmt.Errorf("failed to marshal labels: %w", err)
		}
	}

	if collection.BackupPolicy != nil {
		backupPolicyJSON, err = json.Marshal(collection.BackupPolicy)
		if err != nil {
			return fmt.Errorf("failed to marshal backup policy: %w", err)
		}
	}

	// Get message type name if available
	var messageTypeName string
	if collection.MessageType != nil {
		messageTypeName = collection.MessageType.MessageName
	}

	// Insert or update
	query := `
		INSERT INTO collections (id, namespace, name, db_path, message_type_name, server_endpoint, indexed_fields, labels, backup_policy, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			db_path = excluded.db_path,
			message_type_name = excluded.message_type_name,
			server_endpoint = excluded.server_endpoint,
			indexed_fields = excluded.indexed_fields,
			labels = excluded.labels,
			backup_policy = excluded.backup_policy,
			updated_at = excluded.updated_at
	`

	_, err = s.db.ExecContext(ctx, query,
		id,
		collection.Namespace,
		collection.Name,
		dbPath,
		messageTypeName,
		collection.ServerEndpoint,
		string(indexedFieldsJSON),
		string(labelsJSON),
		string(backupPolicyJSON),
		now,
		now,
	)

	if err != nil {
		return fmt.Errorf("failed to save collection: %w", err)
	}

	return nil
}

// GetCollection retrieves a collection by namespace and name.
func (s *SqliteRegistryStore) GetCollection(ctx context.Context, namespace, name string) (*CollectionMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id := fmt.Sprintf("%s/%s", namespace, name)

	query := `
		SELECT db_path, message_type_name, server_endpoint, indexed_fields, labels, backup_policy
		FROM collections
		WHERE id = ?
	`

	var dbPath, messageTypeName, serverEndpoint, indexedFieldsJSON, labelsJSON, backupPolicyJSON string
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&dbPath,
		&messageTypeName,
		&serverEndpoint,
		&indexedFieldsJSON,
		&labelsJSON,
		&backupPolicyJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("collection %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query collection: %w", err)
	}

	// Reconstruct collection proto
	collection := &pb.Collection{
		Namespace:      namespace,
		Name:           name,
		ServerEndpoint: serverEndpoint,
	}

	if messageTypeName != "" {
		collection.MessageType = &pb.MessageTypeRef{
			MessageName: messageTypeName,
		}
	}

	// Decode indexed fields
	if indexedFieldsJSON != "" {
		var indexedFields []string
		if err := json.Unmarshal([]byte(indexedFieldsJSON), &indexedFields); err == nil {
			collection.IndexedFields = indexedFields
		}
	}

	// Decode labels
	if labelsJSON != "" {
		var labels map[string]string
		if err := json.Unmarshal([]byte(labelsJSON), &labels); err == nil {
			if collection.Metadata == nil {
				collection.Metadata = &pb.Metadata{}
			}
			collection.Metadata.Labels = labels
		}
	}

	// Decode backup policy
	if backupPolicyJSON != "" {
		var backupPolicy pb.BackupPolicy
		if err := json.Unmarshal([]byte(backupPolicyJSON), &backupPolicy); err == nil {
			collection.BackupPolicy = &backupPolicy
		}
	}

	return &CollectionMetadata{
		Collection: collection,
		DBPath:     dbPath,
	}, nil
}

// ListCollections returns all collections, optionally filtered by namespace.
func (s *SqliteRegistryStore) ListCollections(ctx context.Context, namespace string) ([]*CollectionMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var query string
	var args []interface{}

	if namespace != "" {
		query = `SELECT namespace, name, db_path, message_type_name, server_endpoint, indexed_fields, labels, backup_policy FROM collections WHERE namespace = ?`
		args = []interface{}{namespace}
	} else {
		query = `SELECT namespace, name, db_path, message_type_name, server_endpoint, indexed_fields, labels, backup_policy FROM collections`
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}
	defer rows.Close()

	var results []*CollectionMetadata

	for rows.Next() {
		var ns, name, dbPath, messageTypeName, serverEndpoint, indexedFieldsJSON, labelsJSON, backupPolicyJSON string
		if err := rows.Scan(&ns, &name, &dbPath, &messageTypeName, &serverEndpoint, &indexedFieldsJSON, &labelsJSON, &backupPolicyJSON); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		collection := &pb.Collection{
			Namespace:      ns,
			Name:           name,
			ServerEndpoint: serverEndpoint,
		}

		if messageTypeName != "" {
			collection.MessageType = &pb.MessageTypeRef{
				MessageName: messageTypeName,
			}
		}

		// Decode indexed fields
		if indexedFieldsJSON != "" {
			var indexedFields []string
			if err := json.Unmarshal([]byte(indexedFieldsJSON), &indexedFields); err == nil {
				collection.IndexedFields = indexedFields
			}
		}

		// Decode labels
		if labelsJSON != "" {
			var labels map[string]string
			if err := json.Unmarshal([]byte(labelsJSON), &labels); err == nil {
				if collection.Metadata == nil {
					collection.Metadata = &pb.Metadata{}
				}
				collection.Metadata.Labels = labels
			}
		}

		// Decode backup policy
		if backupPolicyJSON != "" {
			var backupPolicy pb.BackupPolicy
			if err := json.Unmarshal([]byte(backupPolicyJSON), &backupPolicy); err == nil {
				collection.BackupPolicy = &backupPolicy
			}
		}

		results = append(results, &CollectionMetadata{
			Collection: collection,
			DBPath:     dbPath,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}

// DeleteCollection removes a collection from the registry.
func (s *SqliteRegistryStore) DeleteCollection(ctx context.Context, namespace, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("%s/%s", namespace, name)

	query := `DELETE FROM collections WHERE id = ?`
	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete collection: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("collection %s not found", id)
	}

	return nil
}

// UpdateMetadata updates the metadata for an existing collection.
func (s *SqliteRegistryStore) UpdateMetadata(ctx context.Context, namespace, name string, meta *pb.Collection) error {
	// Get existing dbPath
	existing, err := s.GetCollection(ctx, namespace, name)
	if err != nil {
		return err
	}

	// Save with existing dbPath
	return s.SaveCollection(ctx, meta, existing.DBPath)
}

// Close closes the registry database connection.
func (s *SqliteRegistryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
