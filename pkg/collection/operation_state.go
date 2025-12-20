package collection

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pb "github.com/accretional/collector/gen/collector"
)

// OperationState represents an in-progress operation on a collection.
// Stored in Collection metadata using reserved label "_operation".
type OperationState struct {
	Type        string `json:"type"`         // "backup", "restore", "delete", "clone"
	URI         string `json:"uri"`          // Path, URL, or identifier for debugging
	StartedAt   int64  `json:"started_at"`   // Unix timestamp
	TimeoutAt   int64  `json:"timeout_at"`   // Unix timestamp (started_at + timeout + padding)
	CollectorID string `json:"collector_id"` // Which collector owns this operation
}

// Operation timeout constants (includes 1 second padding for safety)
const (
	BackupTimeout  = 5*time.Minute + time.Second  // 301s
	RestoreTimeout = 5*time.Minute + time.Second  // 301s
	DeleteTimeout  = 30*time.Second + time.Second // 31s
	CloneTimeout   = 10*time.Minute + time.Second // 601s

	// Reserved label key for operation state
	OperationLabel = "_operation"
)

// ErrOperationInProgress is returned when an operation is already in progress.
type ErrOperationInProgress struct {
	Operation OperationState
}

func (e ErrOperationInProgress) Error() string {
	return fmt.Sprintf("operation in progress: %s (started %s ago, uri: %s)",
		e.Operation.Type,
		time.Since(time.Unix(e.Operation.StartedAt, 0)).Round(time.Second),
		e.Operation.URI)
}

// GetOperationState reads the operation state from collection metadata.
// Returns nil if no operation is in progress.
func GetOperationState(collection *pb.Collection) (*OperationState, error) {
	if collection.Metadata == nil || collection.Metadata.Labels == nil {
		return nil, nil
	}

	opJSON, exists := collection.Metadata.Labels[OperationLabel]
	if !exists || opJSON == "" {
		return nil, nil
	}

	var state OperationState
	if err := json.Unmarshal([]byte(opJSON), &state); err != nil {
		return nil, fmt.Errorf("failed to parse operation state: %w", err)
	}

	return &state, nil
}

// SetOperationState sets the operation state in collection metadata.
func SetOperationState(collection *pb.Collection, state *OperationState) error {
	if collection.Metadata == nil {
		collection.Metadata = &pb.Metadata{}
	}
	if collection.Metadata.Labels == nil {
		collection.Metadata.Labels = make(map[string]string)
	}

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal operation state: %w", err)
	}

	collection.Metadata.Labels[OperationLabel] = string(stateJSON)
	return nil
}

// ClearOperationState removes the operation state from collection metadata.
func ClearOperationState(collection *pb.Collection) {
	if collection.Metadata != nil && collection.Metadata.Labels != nil {
		delete(collection.Metadata.Labels, OperationLabel)
	}
}

// IsOperationTimedOut checks if an operation has exceeded its timeout.
func IsOperationTimedOut(state *OperationState) bool {
	return time.Now().Unix() > state.TimeoutAt
}

// CheckOperationConflict checks if an operation can start.
// Returns ErrOperationInProgress if a non-timed-out operation exists.
func CheckOperationConflict(collection *pb.Collection) error {
	state, err := GetOperationState(collection)
	if err != nil {
		return fmt.Errorf("failed to get operation state: %w", err)
	}

	if state == nil {
		return nil // No operation in progress
	}

	// Check if operation has timed out
	if IsOperationTimedOut(state) {
		// Timed out - clear it and allow new operation
		ClearOperationState(collection)
		return nil
	}

	// Operation still in progress
	return ErrOperationInProgress{Operation: *state}
}

// StartOperation begins a new operation by setting the operation state.
// Returns error if an operation is already in progress.
func StartOperation(ctx context.Context, repo CollectionRepo, namespace, name, opType, uri, collectorID string, timeout time.Duration) error {
	// Get collection metadata
	collection, err := repo.GetCollection(ctx, namespace, name)
	if err != nil {
		return fmt.Errorf("failed to get collection: %w", err)
	}

	// Check for conflicts
	if err := CheckOperationConflict(collection.Meta); err != nil {
		return err
	}

	// Set operation state
	now := time.Now()
	state := &OperationState{
		Type:        opType,
		URI:         uri,
		StartedAt:   now.Unix(),
		TimeoutAt:   now.Add(timeout).Unix(),
		CollectorID: collectorID,
	}

	if err := SetOperationState(collection.Meta, state); err != nil {
		return err
	}

	// Update collection metadata in registry
	if err := repo.UpdateCollectionMetadata(ctx, namespace, name, collection.Meta); err != nil {
		return fmt.Errorf("failed to update operation state: %w", err)
	}

	return nil
}

// CompleteOperation clears the operation state after completion.
// If the collection no longer exists (e.g., it was deleted), this is a no-op and returns nil.
func CompleteOperation(ctx context.Context, repo CollectionRepo, namespace, name string) error {
	collection, err := repo.GetCollection(ctx, namespace, name)
	if err != nil {
		// Collection doesn't exist (likely deleted) - nothing to clean up
		return nil
	}

	ClearOperationState(collection.Meta)

	if err := repo.UpdateCollectionMetadata(ctx, namespace, name, collection.Meta); err != nil {
		return fmt.Errorf("failed to clear operation state: %w", err)
	}

	return nil
}

// CleanupTimedOutOperations scans all collections and clears timed-out operations.
// Should be called on server startup to recover from crashes.
func CleanupTimedOutOperations(ctx context.Context, registryStore RegistryStore) (int, error) {
	// List all collections (pass empty namespace to get all)
	collections, err := registryStore.ListCollections(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("failed to list collections: %w", err)
	}

	cleaned := 0
	for _, metadata := range collections {
		collection := metadata.Collection
		state, err := GetOperationState(collection)
		if err != nil {
			// Log error but continue
			fmt.Printf("Error checking operation state for %s/%s: %v\n",
				collection.Namespace, collection.Name, err)
			continue
		}

		if state != nil && IsOperationTimedOut(state) {
			ClearOperationState(collection)
			if err := registryStore.SaveCollection(ctx, collection, metadata.DBPath); err != nil {
				fmt.Printf("Error clearing timed-out operation for %s/%s: %v\n",
					collection.Namespace, collection.Name, err)
				continue
			}
			fmt.Printf("Cleaned up timed-out %s operation on %s/%s (started %s ago)\n",
				state.Type, collection.Namespace, collection.Name,
				time.Since(time.Unix(state.StartedAt, 0)).Round(time.Second))
			cleaned++
		}
	}

	return cleaned, nil
}
