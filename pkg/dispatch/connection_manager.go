package dispatch

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ActiveConnection represents the runtime state of an active connection.
// This is kept in memory and contains resources that cannot be persisted (gRPC clients).
type ActiveConnection struct {
	ConnectionID string                        // Links to persisted Connection record
	Client       pb.CollectiveDispatcherClient // gRPC client for making calls
	GrpcConn     *grpc.ClientConn              // Underlying gRPC connection
	LastActivity time.Time                     // For timeout/health checks
	IsInitiator  bool                          // True if we initiated this connection

	// Cached connection info for when persistence is unavailable
	Connection *pb.Connection
}

// ConnectionManager manages connections between collectors.
// It maintains both persisted connection records (in system/connections collection)
// and active in-memory connection state (gRPC clients, etc.).
type ConnectionManager struct {
	collectorID string
	address     string
	namespaces  []string
	sessionID   string // Unique per restart, used to detect stale connections
	collection  *collection.Collection

	// Active connections (in-memory runtime state)
	active      map[string]*ActiveConnection
	activeMutex sync.RWMutex

	// Track client connections by address for quick lookup
	clientsByAddress map[string]*ActiveConnection
	clientsMutex     sync.RWMutex
}

// NewConnectionManager creates a new connection manager.
// The sessionID should be unique per collector restart (e.g., UUID or timestamp-based).
func NewConnectionManager(collectorID, address string, namespaces []string, sessionID string, coll *collection.Collection) *ConnectionManager {
	return &ConnectionManager{
		collectorID:      collectorID,
		address:          address,
		namespaces:       namespaces,
		sessionID:        sessionID,
		collection:       coll,
		active:           make(map[string]*ActiveConnection),
		clientsByAddress: make(map[string]*ActiveConnection),
	}
}

// RecoverFromRestart marks connections from previous sessions as STALE.
// This should be called on startup before accepting new connections.
func (cm *ConnectionManager) RecoverFromRestart(ctx context.Context) error {
	if cm.collection == nil {
		return nil // No persistence, nothing to recover
	}

	// Query all connections where we are the source and status is ACTIVE
	// These are from a previous session that crashed
	query := &collection.SearchQuery{
		LabelFilters: map[string]string{
			"source_collector_id": cm.collectorID,
		},
		Limit: 1000, // Reasonable upper bound
	}

	results, err := cm.collection.Search(ctx, query)
	if err != nil {
		return fmt.Errorf("query connections for recovery: %w", err)
	}

	now := timestamppb.Now()
	staleCount := 0

	for _, result := range results {
		var conn pb.Connection
		if err := proto.Unmarshal(result.Record.ProtoData, &conn); err != nil {
			continue // Skip malformed records
		}

		// If this connection is from a different session and still marked ACTIVE, it's stale
		if conn.SessionId != cm.sessionID && conn.Status == pb.Connection_ACTIVE {
			conn.Status = pb.Connection_STALE
			conn.StatusMessage = fmt.Sprintf("Marked stale on restart (previous session: %s)", conn.SessionId)
			conn.DisconnectedAt = now

			if err := cm.updatePersistedConnection(ctx, &conn); err != nil {
				// Log but continue - don't fail startup for persistence errors
				fmt.Printf("Warning: failed to mark connection %s as stale: %v\n", conn.Id, err)
			}
			staleCount++
		}
	}

	if staleCount > 0 {
		fmt.Printf("Marked %d stale connections from previous session\n", staleCount)
	}

	return nil
}

// HandleConnect processes an incoming connection request from another collector.
func (cm *ConnectionManager) HandleConnect(ctx context.Context, req *pb.ConnectRequest) (*pb.ConnectResponse, error) {
	cm.activeMutex.Lock()
	defer cm.activeMutex.Unlock()

	// Validate request
	if req.Address == "" {
		return &pb.ConnectResponse{
			Status: &pb.Status{Code: 400, Message: "address is required"},
		}, nil
	}

	// Find shared namespaces
	sharedNamespaces := cm.findSharedNamespaces(req.Namespaces)

	// Generate connection ID
	connectionID := fmt.Sprintf("conn_%s_%d", req.Address, time.Now().UnixNano())

	// Extract source collector ID from metadata
	sourceCollectorID := "unknown"
	if collectorID, ok := req.Metadata["collector_id"]; ok {
		sourceCollectorID = collectorID
	}

	// Extract source session ID if provided
	sourceSessionID := ""
	if sessID, ok := req.Metadata["session_id"]; ok {
		sourceSessionID = sessID
	}

	now := timestamppb.Now()

	// Check if we have a previous connection from this source
	reconnectCount := int32(0)
	if cm.collection != nil {
		reconnectCount = cm.countPreviousConnections(ctx, sourceCollectorID)
	}

	// Create persisted connection record
	conn := &pb.Connection{
		Id:                connectionID,
		SourceCollectorId: sourceCollectorID,
		TargetCollectorId: cm.collectorID,
		Address:           req.Address,
		SharedNamespaces:  sharedNamespaces,
		Metadata: &pb.Metadata{
			Labels: map[string]string{
				"source_session_id": sourceSessionID,
				"direction":         "inbound",
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		ConnectedAt:    now,
		LastActivity:   now,
		LastHeartbeat:  now,
		SessionId:      cm.sessionID,
		RequestCount:   0,
		ReconnectCount: reconnectCount,
		Status:         pb.Connection_ACTIVE,
	}

	// Store active connection state (no gRPC client for inbound connections)
	cm.active[connectionID] = &ActiveConnection{
		ConnectionID: connectionID,
		LastActivity: time.Now(),
		IsInitiator:  false,
		Connection:   conn, // Cache for non-persisted mode
	}

	// Persist connection record
	if cm.collection != nil {
		if err := cm.persistConnection(ctx, conn); err != nil {
			fmt.Printf("Warning: failed to persist inbound connection: %v\n", err)
		}
	}

	return &pb.ConnectResponse{
		Status: &pb.Status{
			Code:    200,
			Message: fmt.Sprintf("Connected with %d shared namespaces", len(sharedNamespaces)),
		},
		ConnectionId:      connectionID,
		SharedNamespaces:  sharedNamespaces,
		TargetCollectorId: cm.collectorID,
	}, nil
}

// ConnectTo initiates a connection to another collector.
func (cm *ConnectionManager) ConnectTo(ctx context.Context, address string, namespaces []string) (*pb.ConnectResponse, error) {
	// Create gRPC connection
	grpcConn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %w", err)
	}

	// Create dispatcher client
	client := pb.NewCollectiveDispatcherClient(grpcConn)

	// Send connect request with our session ID
	req := &pb.ConnectRequest{
		Address:    cm.address,
		Namespaces: namespaces,
		Metadata: map[string]string{
			"collector_id": cm.collectorID,
			"session_id":   cm.sessionID,
		},
	}

	resp, err := client.Connect(ctx, req)
	if err != nil {
		grpcConn.Close()
		return nil, fmt.Errorf("connect RPC failed: %w", err)
	}

	if resp.Status.Code != 200 {
		grpcConn.Close()
		return resp, fmt.Errorf("connect failed: %s", resp.Status.Message)
	}

	now := timestamppb.Now()

	// Check for previous connections to this target
	reconnectCount := int32(0)
	if cm.collection != nil {
		reconnectCount = cm.countPreviousConnectionsToTarget(ctx, resp.TargetCollectorId)
	}

	// Create persisted connection record
	conn := &pb.Connection{
		Id:                resp.ConnectionId,
		SourceCollectorId: cm.collectorID,
		TargetCollectorId: resp.TargetCollectorId,
		Address:           address,
		SharedNamespaces:  resp.SharedNamespaces,
		Metadata: &pb.Metadata{
			Labels: map[string]string{
				"direction": "outbound",
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		ConnectedAt:    now,
		LastActivity:   now,
		LastHeartbeat:  now,
		SessionId:      cm.sessionID,
		RequestCount:   0,
		ReconnectCount: reconnectCount,
		Status:         pb.Connection_ACTIVE,
	}

	// Create active connection state
	activeConn := &ActiveConnection{
		ConnectionID: resp.ConnectionId,
		Client:       client,
		GrpcConn:     grpcConn,
		LastActivity: time.Now(),
		IsInitiator:  true,
		Connection:   conn, // Cache for non-persisted mode
	}

	// Store in maps
	cm.activeMutex.Lock()
	cm.active[resp.ConnectionId] = activeConn
	cm.activeMutex.Unlock()

	cm.clientsMutex.Lock()
	cm.clientsByAddress[address] = activeConn
	cm.clientsMutex.Unlock()

	// Persist connection record
	if cm.collection != nil {
		if err := cm.persistConnection(ctx, conn); err != nil {
			fmt.Printf("Warning: failed to persist outbound connection: %v\n", err)
		}
	}

	return resp, nil
}

// Disconnect closes a connection and updates the persisted record.
func (cm *ConnectionManager) Disconnect(ctx context.Context, connectionID string, reason string, failed bool) error {
	cm.activeMutex.Lock()
	activeConn, exists := cm.active[connectionID]
	if exists {
		delete(cm.active, connectionID)
	}
	cm.activeMutex.Unlock()

	if !exists {
		return fmt.Errorf("connection %s not found", connectionID)
	}

	// Close gRPC connection if we have one
	if activeConn.GrpcConn != nil {
		activeConn.GrpcConn.Close()

		// Remove from clients by address map
		cm.clientsMutex.Lock()
		for addr, ac := range cm.clientsByAddress {
			if ac.ConnectionID == connectionID {
				delete(cm.clientsByAddress, addr)
				break
			}
		}
		cm.clientsMutex.Unlock()
	}

	// Update persisted record
	if cm.collection != nil {
		conn, err := cm.getPersistedConnection(ctx, connectionID)
		if err != nil {
			return fmt.Errorf("get persisted connection: %w", err)
		}

		now := timestamppb.Now()
		conn.DisconnectedAt = now
		conn.Metadata.UpdatedAt = now
		conn.StatusMessage = reason

		if failed {
			conn.Status = pb.Connection_FAILED
		} else {
			conn.Status = pb.Connection_DISCONNECTED
		}

		if err := cm.updatePersistedConnection(ctx, conn); err != nil {
			return fmt.Errorf("update persisted connection: %w", err)
		}
	}

	return nil
}

// RecordRequest increments the request count and updates activity for a connection.
func (cm *ConnectionManager) RecordRequest(ctx context.Context, connectionID string, bytesSent, bytesReceived int64) {
	cm.activeMutex.Lock()
	if activeConn, ok := cm.active[connectionID]; ok {
		activeConn.LastActivity = time.Now()
	}
	cm.activeMutex.Unlock()

	// Update persisted stats (debounce this in production for performance)
	if cm.collection != nil {
		conn, err := cm.getPersistedConnection(ctx, connectionID)
		if err != nil {
			return // Silently fail - stats update shouldn't break requests
		}

		conn.RequestCount++
		conn.BytesSent += bytesSent
		conn.BytesReceived += bytesReceived
		conn.LastActivity = timestamppb.Now()

		cm.updatePersistedConnection(ctx, conn)
	}
}

// GetClient returns a client for the given address.
func (cm *ConnectionManager) GetClient(address string) (pb.CollectiveDispatcherClient, bool) {
	cm.clientsMutex.RLock()
	defer cm.clientsMutex.RUnlock()

	if activeConn, ok := cm.clientsByAddress[address]; ok {
		return activeConn.Client, true
	}
	return nil, false
}

// GetActiveConnection returns an active connection by ID.
func (cm *ConnectionManager) GetActiveConnection(connectionID string) (*ActiveConnection, bool) {
	cm.activeMutex.RLock()
	defer cm.activeMutex.RUnlock()

	conn, ok := cm.active[connectionID]
	return conn, ok
}

// ListActiveConnections returns all active connections.
func (cm *ConnectionManager) ListActiveConnections() []*ActiveConnection {
	cm.activeMutex.RLock()
	defer cm.activeMutex.RUnlock()

	connections := make([]*ActiveConnection, 0, len(cm.active))
	for _, conn := range cm.active {
		connections = append(connections, conn)
	}
	return connections
}

// ListConnections returns Connection protos for all active connections.
// If persistence is enabled, this loads the persisted data.
// Otherwise, it uses the cached Connection from in-memory state.
func (cm *ConnectionManager) ListConnections() []*pb.Connection {
	cm.activeMutex.RLock()
	defer cm.activeMutex.RUnlock()

	connections := make([]*pb.Connection, 0, len(cm.active))
	ctx := context.Background()

	for id, activeConn := range cm.active {
		if cm.collection != nil {
			// Load full persisted data
			conn, err := cm.getPersistedConnection(ctx, id)
			if err == nil {
				connections = append(connections, conn)
				continue
			}
		}

		// Fall back to cached in-memory data
		if activeConn.Connection != nil {
			// Update last activity from in-memory state
			activeConn.Connection.LastActivity = timestamppb.New(activeConn.LastActivity)
			connections = append(connections, activeConn.Connection)
		} else {
			// Minimal fallback if no cached connection
			conn := &pb.Connection{
				Id:           id,
				LastActivity: timestamppb.New(activeConn.LastActivity),
				Status:       pb.Connection_ACTIVE,
			}
			connections = append(connections, conn)
		}
	}

	return connections
}

// CloseAll closes all active connections.
func (cm *ConnectionManager) CloseAll() {
	cm.activeMutex.Lock()
	defer cm.activeMutex.Unlock()

	ctx := context.Background()

	for connID, activeConn := range cm.active {
		if activeConn.GrpcConn != nil {
			activeConn.GrpcConn.Close()
		}

		// Update persisted record
		if cm.collection != nil {
			if conn, err := cm.getPersistedConnection(ctx, connID); err == nil {
				conn.Status = pb.Connection_DISCONNECTED
				conn.DisconnectedAt = timestamppb.Now()
				conn.StatusMessage = "Shutdown"
				cm.updatePersistedConnection(ctx, conn)
			}
		}
	}

	cm.active = make(map[string]*ActiveConnection)

	cm.clientsMutex.Lock()
	cm.clientsByAddress = make(map[string]*ActiveConnection)
	cm.clientsMutex.Unlock()
}

// persistConnection saves a new connection to the collection.
func (cm *ConnectionManager) persistConnection(ctx context.Context, conn *pb.Connection) error {
	protoData, err := proto.Marshal(conn)
	if err != nil {
		return fmt.Errorf("marshal connection: %w", err)
	}

	// Add searchable labels
	labels := make(map[string]string)
	if conn.Metadata != nil && conn.Metadata.Labels != nil {
		for k, v := range conn.Metadata.Labels {
			labels[k] = v
		}
	}
	labels["source_collector_id"] = conn.SourceCollectorId
	labels["target_collector_id"] = conn.TargetCollectorId
	labels["session_id"] = conn.SessionId
	labels["status"] = conn.Status.String()

	record := &pb.CollectionRecord{
		Id:        conn.Id,
		ProtoData: protoData,
		Metadata: &pb.Metadata{
			Labels:    labels,
			CreatedAt: conn.Metadata.CreatedAt,
			UpdatedAt: conn.Metadata.UpdatedAt,
		},
	}

	return cm.collection.CreateRecord(ctx, record)
}

// updatePersistedConnection updates an existing connection record.
func (cm *ConnectionManager) updatePersistedConnection(ctx context.Context, conn *pb.Connection) error {
	// Ensure metadata exists
	if conn.Metadata == nil {
		conn.Metadata = &pb.Metadata{
			Labels:    make(map[string]string),
			CreatedAt: timestamppb.Now(),
		}
	}
	conn.Metadata.UpdatedAt = timestamppb.Now()

	protoData, err := proto.Marshal(conn)
	if err != nil {
		return fmt.Errorf("marshal connection: %w", err)
	}

	// Update labels
	labels := make(map[string]string)
	if conn.Metadata.Labels != nil {
		for k, v := range conn.Metadata.Labels {
			labels[k] = v
		}
	}
	labels["source_collector_id"] = conn.SourceCollectorId
	labels["target_collector_id"] = conn.TargetCollectorId
	labels["session_id"] = conn.SessionId
	labels["status"] = conn.Status.String()

	record := &pb.CollectionRecord{
		Id:        conn.Id,
		ProtoData: protoData,
		Metadata: &pb.Metadata{
			Labels:    labels,
			CreatedAt: conn.Metadata.CreatedAt,
			UpdatedAt: conn.Metadata.UpdatedAt,
		},
	}

	return cm.collection.UpdateRecord(ctx, record)
}

// getPersistedConnection retrieves a connection record from the collection.
func (cm *ConnectionManager) getPersistedConnection(ctx context.Context, connectionID string) (*pb.Connection, error) {
	record, err := cm.collection.GetRecord(ctx, connectionID)
	if err != nil {
		return nil, err
	}

	var conn pb.Connection
	if err := proto.Unmarshal(record.ProtoData, &conn); err != nil {
		return nil, fmt.Errorf("unmarshal connection: %w", err)
	}

	return &conn, nil
}

// countPreviousConnections counts how many times a source has connected to us.
func (cm *ConnectionManager) countPreviousConnections(ctx context.Context, sourceCollectorID string) int32 {
	query := &collection.SearchQuery{
		LabelFilters: map[string]string{
			"source_collector_id": sourceCollectorID,
			"target_collector_id": cm.collectorID,
		},
		Limit: 1000,
	}

	results, err := cm.collection.Search(ctx, query)
	if err != nil {
		return 0
	}

	return int32(len(results))
}

// countPreviousConnectionsToTarget counts how many times we've connected to a target.
func (cm *ConnectionManager) countPreviousConnectionsToTarget(ctx context.Context, targetCollectorID string) int32 {
	query := &collection.SearchQuery{
		LabelFilters: map[string]string{
			"source_collector_id": cm.collectorID,
			"target_collector_id": targetCollectorID,
		},
		Limit: 1000,
	}

	results, err := cm.collection.Search(ctx, query)
	if err != nil {
		return 0
	}

	return int32(len(results))
}

// findSharedNamespaces finds namespaces that are in both lists.
func (cm *ConnectionManager) findSharedNamespaces(requestedNamespaces []string) []string {
	if len(cm.namespaces) == 0 || len(requestedNamespaces) == 0 {
		return []string{}
	}

	ourNamespaces := make(map[string]bool)
	for _, ns := range cm.namespaces {
		ourNamespaces[ns] = true
	}

	var shared []string
	for _, ns := range requestedNamespaces {
		if ourNamespaces[ns] {
			shared = append(shared, ns)
		}
	}

	return shared
}
