package dispatch

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ConnectionManager manages connections between collectors
type ConnectionManager struct {
	collectorID string
	address     string
	namespaces  []string

	// Track active connections
	connections      map[string]*ConnectionState
	connectionsMutex sync.RWMutex

	// Track client connections to other collectors
	clients      map[string]pb.CollectiveDispatcherClient
	clientsMutex sync.RWMutex
}

// ConnectionState represents an active connection
type ConnectionState struct {
	Connection   *pb.Connection
	Client       pb.CollectiveDispatcherClient
	GrpcConn     *grpc.ClientConn
	LastActivity time.Time
}

// NewConnectionManager creates a new connection manager
func NewConnectionManager(collectorID, address string, namespaces []string) *ConnectionManager {
	return &ConnectionManager{
		collectorID: collectorID,
		address:     address,
		namespaces:  namespaces,
		connections: make(map[string]*ConnectionState),
		clients:     make(map[string]pb.CollectiveDispatcherClient),
	}
}

// HandleConnect processes an incoming connection request
func (cm *ConnectionManager) HandleConnect(ctx context.Context, req *pb.ConnectRequest) (*pb.ConnectResponse, error) {
	cm.connectionsMutex.Lock()
	defer cm.connectionsMutex.Unlock()

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

	// Create connection record
	conn := &pb.Connection{
		Id:                connectionID,
		SourceCollectorId: "remote", // Will be updated when we know the source
		TargetCollectorId: cm.collectorID,
		Address:           req.Address,
		SharedNamespaces:  sharedNamespaces,
		Metadata: &pb.Metadata{
			Labels:    req.Metadata,
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
		},
		LastActivity: timestamppb.Now(),
	}

	// Store connection state
	cm.connections[connectionID] = &ConnectionState{
		Connection:   conn,
		LastActivity: time.Now(),
	}

	return &pb.ConnectResponse{
		Status: &pb.Status{
			Code:    200,
			Message: fmt.Sprintf("Connected with %d shared namespaces", len(sharedNamespaces)),
		},
		ConnectionId:     connectionID,
		SharedNamespaces: sharedNamespaces,
	}, nil
}

// ConnectTo initiates a connection to another collector
func (cm *ConnectionManager) ConnectTo(ctx context.Context, address string, namespaces []string) (*pb.ConnectResponse, error) {
	// Create gRPC connection
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %w", err)
	}

	// Create dispatcher client
	client := pb.NewCollectiveDispatcherClient(conn)

	// Send connect request
	req := &pb.ConnectRequest{
		Address:    cm.address,
		Namespaces: namespaces,
		Metadata: map[string]string{
			"collector_id": cm.collectorID,
		},
	}

	resp, err := client.Connect(ctx, req)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("connect RPC failed: %w", err)
	}

	if resp.Status.Code != 200 {
		conn.Close()
		return resp, fmt.Errorf("connect failed: %s", resp.Status.Message)
	}

	// Store client connection
	cm.clientsMutex.Lock()
	cm.clients[address] = client
	cm.clientsMutex.Unlock()

	// Store connection state with shared namespaces from the response
	connState := &ConnectionState{
		Connection: &pb.Connection{
			Id:                resp.ConnectionId,
			SourceCollectorId: cm.collectorID,
			TargetCollectorId: "remote",
			Address:           address,
			SharedNamespaces:  resp.SharedNamespaces,
			Metadata: &pb.Metadata{
				Labels:    map[string]string{"initiator": "true"},
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
			LastActivity: timestamppb.Now(),
		},
		Client:       client,
		GrpcConn:     conn,
		LastActivity: time.Now(),
	}

	cm.connectionsMutex.Lock()
	cm.connections[resp.ConnectionId] = connState
	cm.connectionsMutex.Unlock()

	return resp, nil
}

// GetClient returns a client for the given address
func (cm *ConnectionManager) GetClient(address string) (pb.CollectiveDispatcherClient, bool) {
	cm.clientsMutex.RLock()
	defer cm.clientsMutex.RUnlock()

	client, ok := cm.clients[address]
	return client, ok
}

// GetConnection returns a connection by ID
func (cm *ConnectionManager) GetConnection(connectionID string) (*ConnectionState, bool) {
	cm.connectionsMutex.RLock()
	defer cm.connectionsMutex.RUnlock()

	conn, ok := cm.connections[connectionID]
	return conn, ok
}

// ListConnections returns all active connections
func (cm *ConnectionManager) ListConnections() []*pb.Connection {
	cm.connectionsMutex.RLock()
	defer cm.connectionsMutex.RUnlock()

	connections := make([]*pb.Connection, 0, len(cm.connections))
	for _, state := range cm.connections {
		connections = append(connections, state.Connection)
	}

	return connections
}

// UpdateActivity updates the last activity time for a connection
func (cm *ConnectionManager) UpdateActivity(connectionID string) {
	cm.connectionsMutex.Lock()
	defer cm.connectionsMutex.Unlock()

	if state, ok := cm.connections[connectionID]; ok {
		state.LastActivity = time.Now()
		state.Connection.LastActivity = timestamppb.Now()
	}
}

// CloseAll closes all connections
func (cm *ConnectionManager) CloseAll() {
	cm.connectionsMutex.Lock()
	defer cm.connectionsMutex.Unlock()

	for _, state := range cm.connections {
		if state.GrpcConn != nil {
			state.GrpcConn.Close()
		}
	}

	cm.connections = make(map[string]*ConnectionState)
	cm.clients = make(map[string]pb.CollectiveDispatcherClient)
}

// findSharedNamespaces finds namespaces that are in both lists
func (cm *ConnectionManager) findSharedNamespaces(requestedNamespaces []string) []string {
	if len(cm.namespaces) == 0 || len(requestedNamespaces) == 0 {
		return []string{}
	}

	// Create a set of our namespaces for fast lookup
	ourNamespaces := make(map[string]bool)
	for _, ns := range cm.namespaces {
		ourNamespaces[ns] = true
	}

	// Find shared namespaces
	var shared []string
	for _, ns := range requestedNamespaces {
		if ourNamespaces[ns] {
			shared = append(shared, ns)
		}
	}

	return shared
}
