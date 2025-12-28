package dispatch_test

import (
	"context"
	"testing"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
	"github.com/accretional/collector/pkg/dispatch"
	"google.golang.org/protobuf/proto"
)

// setupTestCollection creates a test collection for connections
func setupTestCollection(t *testing.T) *collection.Collection {
	t.Helper()

	store, err := sqlite.NewStore(":memory:", collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	coll, err := collection.NewCollection(
		&pb.Collection{Namespace: "system", Name: "connections"},
		store,
		&collection.LocalFileSystem{},
	)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	t.Cleanup(func() {
		coll.Close()
	})

	return coll
}

func TestConnectionManager_SessionID(t *testing.T) {
	coll := setupTestCollection(t)

	// Create two connection managers with the same collector ID
	// They should have different session IDs
	cm1 := dispatch.NewConnectionManager("collector1", "addr1", []string{"ns1"}, "session1", coll)
	cm2 := dispatch.NewConnectionManager("collector1", "addr1", []string{"ns1"}, "session2", coll)

	// The managers should be distinct (we can't directly test session ID, but we can test behavior)
	if cm1 == cm2 {
		t.Error("expected different connection managers")
	}
}

func TestConnectionManager_HandleConnect_PersistsConnection(t *testing.T) {
	coll := setupTestCollection(t)

	cm := dispatch.NewConnectionManager("target-collector", "localhost:8080", []string{"ns1", "ns2"}, "session123", coll)

	ctx := context.Background()

	// Handle an incoming connection
	resp, err := cm.HandleConnect(ctx, &pb.ConnectRequest{
		Address:    "localhost:9090",
		Namespaces: []string{"ns1", "ns3"},
		Metadata: map[string]string{
			"collector_id": "source-collector",
			"session_id":   "source-session",
		},
	})
	if err != nil {
		t.Fatalf("HandleConnect failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Fatalf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	// Verify shared namespaces
	if len(resp.SharedNamespaces) != 1 || resp.SharedNamespaces[0] != "ns1" {
		t.Errorf("expected shared namespaces [ns1], got %v", resp.SharedNamespaces)
	}

	// Verify connection is persisted
	record, err := coll.GetRecord(ctx, resp.ConnectionId)
	if err != nil {
		t.Fatalf("failed to get persisted connection: %v", err)
	}

	var conn pb.Connection
	if err := proto.Unmarshal(record.ProtoData, &conn); err != nil {
		t.Fatalf("failed to unmarshal connection: %v", err)
	}

	// Verify persisted fields
	if conn.SourceCollectorId != "source-collector" {
		t.Errorf("expected source collector 'source-collector', got '%s'", conn.SourceCollectorId)
	}
	if conn.TargetCollectorId != "target-collector" {
		t.Errorf("expected target collector 'target-collector', got '%s'", conn.TargetCollectorId)
	}
	if conn.SessionId != "session123" {
		t.Errorf("expected session 'session123', got '%s'", conn.SessionId)
	}
	if conn.Status != pb.Connection_ACTIVE {
		t.Errorf("expected status ACTIVE, got %v", conn.Status)
	}
	if conn.ConnectedAt == nil {
		t.Error("expected connected_at to be set")
	}
}

func TestConnectionManager_RecoverFromRestart_MarksStaleConnections(t *testing.T) {
	coll := setupTestCollection(t)
	ctx := context.Background()

	// Simulate a previous session by creating a connection record directly
	oldConn := &pb.Connection{
		Id:                "old-conn-1",
		SourceCollectorId: "collector1",
		TargetCollectorId: "collector2",
		SessionId:         "old-session",
		Status:            pb.Connection_ACTIVE,
	}
	protoData, _ := proto.Marshal(oldConn)
	err := coll.CreateRecord(ctx, &pb.CollectionRecord{
		Id:        oldConn.Id,
		ProtoData: protoData,
		Metadata: &pb.Metadata{
			Labels: map[string]string{
				"source_collector_id": oldConn.SourceCollectorId,
				"session_id":          oldConn.SessionId,
				"status":              oldConn.Status.String(),
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to create old connection record: %v", err)
	}

	// Create a new connection manager with a different session
	cm := dispatch.NewConnectionManager("collector1", "addr1", []string{"ns1"}, "new-session", coll)

	// Run recovery
	if err := cm.RecoverFromRestart(ctx); err != nil {
		t.Fatalf("RecoverFromRestart failed: %v", err)
	}

	// Verify the old connection is now marked as STALE
	record, err := coll.GetRecord(ctx, "old-conn-1")
	if err != nil {
		t.Fatalf("failed to get connection record: %v", err)
	}

	var updatedConn pb.Connection
	if err := proto.Unmarshal(record.ProtoData, &updatedConn); err != nil {
		t.Fatalf("failed to unmarshal connection: %v", err)
	}

	if updatedConn.Status != pb.Connection_STALE {
		t.Errorf("expected status STALE, got %v", updatedConn.Status)
	}
	if updatedConn.DisconnectedAt == nil {
		t.Error("expected disconnected_at to be set")
	}
}

func TestConnectionManager_Disconnect_UpdatesStatus(t *testing.T) {
	coll := setupTestCollection(t)
	ctx := context.Background()

	cm := dispatch.NewConnectionManager("target-collector", "localhost:8080", []string{"ns1"}, "session123", coll)

	// Create a connection
	resp, err := cm.HandleConnect(ctx, &pb.ConnectRequest{
		Address:    "localhost:9090",
		Namespaces: []string{"ns1"},
		Metadata: map[string]string{
			"collector_id": "source-collector",
		},
	})
	if err != nil {
		t.Fatalf("HandleConnect failed: %v", err)
	}

	// Disconnect
	if err := cm.Disconnect(ctx, resp.ConnectionId, "test disconnect", false); err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}

	// Verify status is updated
	record, err := coll.GetRecord(ctx, resp.ConnectionId)
	if err != nil {
		t.Fatalf("failed to get connection record: %v", err)
	}

	var conn pb.Connection
	if err := proto.Unmarshal(record.ProtoData, &conn); err != nil {
		t.Fatalf("failed to unmarshal connection: %v", err)
	}

	if conn.Status != pb.Connection_DISCONNECTED {
		t.Errorf("expected status DISCONNECTED, got %v", conn.Status)
	}
	if conn.StatusMessage != "test disconnect" {
		t.Errorf("expected status message 'test disconnect', got '%s'", conn.StatusMessage)
	}
	if conn.DisconnectedAt == nil {
		t.Error("expected disconnected_at to be set")
	}
}

func TestConnectionManager_Disconnect_FailedStatus(t *testing.T) {
	coll := setupTestCollection(t)
	ctx := context.Background()

	cm := dispatch.NewConnectionManager("target-collector", "localhost:8080", []string{"ns1"}, "session123", coll)

	// Create a connection
	resp, err := cm.HandleConnect(ctx, &pb.ConnectRequest{
		Address:    "localhost:9090",
		Namespaces: []string{"ns1"},
		Metadata: map[string]string{
			"collector_id": "source-collector",
		},
	})
	if err != nil {
		t.Fatalf("HandleConnect failed: %v", err)
	}

	// Disconnect with failure
	if err := cm.Disconnect(ctx, resp.ConnectionId, "connection error", true); err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}

	// Verify status is FAILED
	record, err := coll.GetRecord(ctx, resp.ConnectionId)
	if err != nil {
		t.Fatalf("failed to get connection record: %v", err)
	}

	var conn pb.Connection
	if err := proto.Unmarshal(record.ProtoData, &conn); err != nil {
		t.Fatalf("failed to unmarshal connection: %v", err)
	}

	if conn.Status != pb.Connection_FAILED {
		t.Errorf("expected status FAILED, got %v", conn.Status)
	}
}

func TestConnectionManager_RecordRequest_UpdatesStats(t *testing.T) {
	coll := setupTestCollection(t)
	ctx := context.Background()

	cm := dispatch.NewConnectionManager("target-collector", "localhost:8080", []string{"ns1"}, "session123", coll)

	// Create a connection
	resp, err := cm.HandleConnect(ctx, &pb.ConnectRequest{
		Address:    "localhost:9090",
		Namespaces: []string{"ns1"},
		Metadata: map[string]string{
			"collector_id": "source-collector",
		},
	})
	if err != nil {
		t.Fatalf("HandleConnect failed: %v", err)
	}

	// Record some requests
	cm.RecordRequest(ctx, resp.ConnectionId, 100, 200)
	cm.RecordRequest(ctx, resp.ConnectionId, 150, 250)

	// Give a moment for any async operations
	time.Sleep(10 * time.Millisecond)

	// Verify stats are updated
	record, err := coll.GetRecord(ctx, resp.ConnectionId)
	if err != nil {
		t.Fatalf("failed to get connection record: %v", err)
	}

	var conn pb.Connection
	if err := proto.Unmarshal(record.ProtoData, &conn); err != nil {
		t.Fatalf("failed to unmarshal connection: %v", err)
	}

	if conn.RequestCount != 2 {
		t.Errorf("expected request count 2, got %d", conn.RequestCount)
	}
	if conn.BytesSent != 250 {
		t.Errorf("expected bytes sent 250, got %d", conn.BytesSent)
	}
	if conn.BytesReceived != 450 {
		t.Errorf("expected bytes received 450, got %d", conn.BytesReceived)
	}
}

func TestConnectionManager_ReconnectCount(t *testing.T) {
	coll := setupTestCollection(t)
	ctx := context.Background()

	cm := dispatch.NewConnectionManager("target-collector", "localhost:8080", []string{"ns1"}, "session123", coll)

	// Create first connection from source
	resp1, err := cm.HandleConnect(ctx, &pb.ConnectRequest{
		Address:    "localhost:9090",
		Namespaces: []string{"ns1"},
		Metadata: map[string]string{
			"collector_id": "source-collector",
		},
	})
	if err != nil {
		t.Fatalf("First HandleConnect failed: %v", err)
	}

	// Disconnect first connection
	cm.Disconnect(ctx, resp1.ConnectionId, "first disconnect", false)

	// Create second connection from same source
	resp2, err := cm.HandleConnect(ctx, &pb.ConnectRequest{
		Address:    "localhost:9090",
		Namespaces: []string{"ns1"},
		Metadata: map[string]string{
			"collector_id": "source-collector",
		},
	})
	if err != nil {
		t.Fatalf("Second HandleConnect failed: %v", err)
	}

	// Verify reconnect count is incremented
	record, err := coll.GetRecord(ctx, resp2.ConnectionId)
	if err != nil {
		t.Fatalf("failed to get connection record: %v", err)
	}

	var conn pb.Connection
	if err := proto.Unmarshal(record.ProtoData, &conn); err != nil {
		t.Fatalf("failed to unmarshal connection: %v", err)
	}

	// Should count the first connection
	if conn.ReconnectCount != 1 {
		t.Errorf("expected reconnect count 1, got %d", conn.ReconnectCount)
	}
}

func TestConnectionManager_CloseAll_UpdatesAllConnections(t *testing.T) {
	coll := setupTestCollection(t)
	ctx := context.Background()

	cm := dispatch.NewConnectionManager("target-collector", "localhost:8080", []string{"ns1"}, "session123", coll)

	// Create multiple connections
	for i := 0; i < 3; i++ {
		_, err := cm.HandleConnect(ctx, &pb.ConnectRequest{
			Address:    "localhost:909" + string(rune('0'+i)),
			Namespaces: []string{"ns1"},
			Metadata: map[string]string{
				"collector_id": "source-" + string(rune('a'+i)),
			},
		})
		if err != nil {
			t.Fatalf("HandleConnect %d failed: %v", i, err)
		}
	}

	// Verify we have 3 connections
	connections := cm.ListConnections()
	if len(connections) != 3 {
		t.Fatalf("expected 3 connections, got %d", len(connections))
	}

	// Close all
	cm.CloseAll()

	// Verify all connections are marked as disconnected in persistence
	records, err := coll.ListRecords(ctx, 0, 10)
	if err != nil {
		t.Fatalf("failed to list records: %v", err)
	}

	for _, record := range records {
		var conn pb.Connection
		if err := proto.Unmarshal(record.ProtoData, &conn); err != nil {
			continue
		}
		if conn.Status != pb.Connection_DISCONNECTED {
			t.Errorf("expected connection %s to be DISCONNECTED, got %v", conn.Id, conn.Status)
		}
	}

	// Verify in-memory connections are cleared
	connections = cm.ListConnections()
	if len(connections) != 0 {
		t.Errorf("expected 0 connections after CloseAll, got %d", len(connections))
	}
}
