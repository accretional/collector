package collection

import (
	"encoding/json"
	"testing"
	"time"

	pb "github.com/accretional/collector/gen/collector"
)

func TestOperationState_SetGetClear(t *testing.T) {
	collection := &pb.Collection{
		Namespace: "test",
		Name:      "users",
		Metadata:  &pb.Metadata{},
	}

	// Initially no operation
	state, err := GetOperationState(collection)
	if err != nil {
		t.Fatalf("GetOperationState failed: %v", err)
	}
	if state != nil {
		t.Errorf("Expected no operation state, got: %+v", state)
	}

	// Set operation state
	now := time.Now()
	opState := &OperationState{
		Type:        "backup",
		URI:         "/data/.backup/test/users-123.db",
		StartedAt:   now.Unix(),
		TimeoutAt:   now.Add(5 * time.Minute).Unix(),
		CollectorID: "collector-001",
	}

	if err := SetOperationState(collection, opState); err != nil {
		t.Fatalf("SetOperationState failed: %v", err)
	}

	// Get operation state
	retrieved, err := GetOperationState(collection)
	if err != nil {
		t.Fatalf("GetOperationState failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected operation state, got nil")
	}

	if retrieved.Type != opState.Type {
		t.Errorf("Type mismatch: got %s, want %s", retrieved.Type, opState.Type)
	}
	if retrieved.URI != opState.URI {
		t.Errorf("URI mismatch: got %s, want %s", retrieved.URI, opState.URI)
	}
	if retrieved.CollectorID != opState.CollectorID {
		t.Errorf("CollectorID mismatch: got %s, want %s", retrieved.CollectorID, opState.CollectorID)
	}

	// Clear operation state
	ClearOperationState(collection)

	cleared, err := GetOperationState(collection)
	if err != nil {
		t.Fatalf("GetOperationState after clear failed: %v", err)
	}
	if cleared != nil {
		t.Errorf("Expected no operation state after clear, got: %+v", cleared)
	}
}

func TestOperationState_JSONSerialization(t *testing.T) {
	state := &OperationState{
		Type:        "restore",
		URI:         "restore:backup-123->prod/users",
		StartedAt:   1703001234,
		TimeoutAt:   1703001534,
		CollectorID: "/data",
	}

	// Serialize
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}

	// Deserialize
	var deserialized OperationState
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	// Verify
	if deserialized.Type != state.Type {
		t.Errorf("Type mismatch after deserialization")
	}
	if deserialized.URI != state.URI {
		t.Errorf("URI mismatch after deserialization")
	}
	if deserialized.StartedAt != state.StartedAt {
		t.Errorf("StartedAt mismatch after deserialization")
	}
	if deserialized.TimeoutAt != state.TimeoutAt {
		t.Errorf("TimeoutAt mismatch after deserialization")
	}
}

func TestOperationState_Timeout(t *testing.T) {
	// Create expired operation
	expired := &OperationState{
		Type:      "backup",
		URI:       "/test",
		StartedAt: time.Now().Add(-10 * time.Minute).Unix(),
		TimeoutAt: time.Now().Add(-5 * time.Minute).Unix(),
	}

	if !IsOperationTimedOut(expired) {
		t.Error("Expected operation to be timed out")
	}

	// Create active operation
	active := &OperationState{
		Type:      "backup",
		URI:       "/test",
		StartedAt: time.Now().Unix(),
		TimeoutAt: time.Now().Add(5 * time.Minute).Unix(),
	}

	if IsOperationTimedOut(active) {
		t.Error("Expected operation to not be timed out")
	}
}

func TestCheckOperationConflict_NoOperation(t *testing.T) {
	collection := &pb.Collection{
		Namespace: "test",
		Name:      "users",
		Metadata:  &pb.Metadata{},
	}

	// Should allow operation when no operation is in progress
	if err := CheckOperationConflict(collection); err != nil {
		t.Errorf("Expected no conflict, got: %v", err)
	}
}

func TestCheckOperationConflict_ActiveOperation(t *testing.T) {
	collection := &pb.Collection{
		Namespace: "test",
		Name:      "users",
		Metadata:  &pb.Metadata{},
	}

	// Set active operation
	now := time.Now()
	state := &OperationState{
		Type:      "backup",
		URI:       "/data/.backup/test/users-123.db",
		StartedAt: now.Unix(),
		TimeoutAt: now.Add(5 * time.Minute).Unix(),
	}
	if err := SetOperationState(collection, state); err != nil {
		t.Fatalf("SetOperationState failed: %v", err)
	}

	// Should return conflict
	err := CheckOperationConflict(collection)
	if err == nil {
		t.Fatal("Expected operation conflict, got nil")
	}

	opErr, ok := err.(ErrOperationInProgress)
	if !ok {
		t.Fatalf("Expected ErrOperationInProgress, got: %T", err)
	}

	if opErr.Operation.Type != "backup" {
		t.Errorf("Expected backup operation in error, got: %s", opErr.Operation.Type)
	}
}

func TestCheckOperationConflict_TimedOutOperation(t *testing.T) {
	collection := &pb.Collection{
		Namespace: "test",
		Name:      "users",
		Metadata:  &pb.Metadata{Labels: make(map[string]string)},
	}

	// Set timed-out operation
	state := &OperationState{
		Type:      "backup",
		URI:       "/test",
		StartedAt: time.Now().Add(-10 * time.Minute).Unix(),
		TimeoutAt: time.Now().Add(-1 * time.Second).Unix(), // Timed out
	}
	if err := SetOperationState(collection, state); err != nil {
		t.Fatalf("SetOperationState failed: %v", err)
	}

	// Should clear timed-out operation and allow new operation
	if err := CheckOperationConflict(collection); err != nil {
		t.Errorf("Expected no conflict (timed out operation should be cleared), got: %v", err)
	}

	// Verify operation was cleared
	remaining, err := GetOperationState(collection)
	if err != nil {
		t.Fatalf("GetOperationState failed: %v", err)
	}
	if remaining != nil {
		t.Errorf("Expected timed-out operation to be cleared, but still present: %+v", remaining)
	}
}

func TestErrOperationInProgress_Error(t *testing.T) {
	state := OperationState{
		Type:      "backup",
		URI:       "/data/.backup/test/users-123.db",
		StartedAt: time.Now().Add(-2 * time.Minute).Unix(),
	}

	err := ErrOperationInProgress{Operation: state}
	errMsg := err.Error()

	// Should include operation type and time
	if errMsg == "" {
		t.Error("Error message is empty")
	}

	// Check that message contains key information
	t.Logf("Error message: %s", errMsg)
}

func TestOperationTimeouts(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{"BackupTimeout", BackupTimeout, 5*time.Minute + time.Second},
		{"RestoreTimeout", RestoreTimeout, 5*time.Minute + time.Second},
		{"DeleteTimeout", DeleteTimeout, 30*time.Second + time.Second},
		{"CloneTimeout", CloneTimeout, 10*time.Minute + time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.timeout, tt.want)
			}

			// Verify padding (should be > base timeout)
			baseDuration := tt.want - time.Second
			if tt.timeout <= baseDuration {
				t.Errorf("%s should include 1s padding", tt.name)
			}
		})
	}
}

func TestOperationState_NilMetadata(t *testing.T) {
	// Test with nil metadata
	collection := &pb.Collection{
		Namespace: "test",
		Name:      "users",
		Metadata:  nil,
	}

	state, err := GetOperationState(collection)
	if err != nil {
		t.Fatalf("GetOperationState with nil metadata failed: %v", err)
	}
	if state != nil {
		t.Errorf("Expected nil state, got: %+v", state)
	}

	// Setting state should create metadata
	newState := &OperationState{
		Type:      "backup",
		StartedAt: time.Now().Unix(),
		TimeoutAt: time.Now().Add(5 * time.Minute).Unix(),
	}

	if err := SetOperationState(collection, newState); err != nil {
		t.Fatalf("SetOperationState with nil metadata failed: %v", err)
	}

	if collection.Metadata == nil {
		t.Error("Expected metadata to be created")
	}
	if collection.Metadata.Labels == nil {
		t.Error("Expected labels to be created")
	}
}

func TestOperationState_InvalidJSON(t *testing.T) {
	collection := &pb.Collection{
		Namespace: "test",
		Name:      "users",
		Metadata: &pb.Metadata{
			Labels: map[string]string{
				OperationLabel: "invalid json {",
			},
		},
	}

	_, err := GetOperationState(collection)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}
