package dispatch_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/dispatch"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/anypb"
)

const bufSize = 1024 * 1024

// testServer wraps a dispatcher with a gRPC server
type testServer struct {
	dispatcher *dispatch.Dispatcher
	grpcServer *grpc.Server
	listener   *bufconn.Listener
	address    string
}

// setupTestServer creates a test gRPC server with a dispatcher
func setupTestServer(t *testing.T, collectorID string, namespaces []string) *testServer {
	t.Helper()

	listener := bufconn.Listen(bufSize)
	address := fmt.Sprintf("bufconn-%s", collectorID)

	grpcServer := grpc.NewServer()
	dispatcher := dispatch.NewDispatcher(collectorID, address, namespaces)

	pb.RegisterCollectiveDispatcherServer(grpcServer, dispatcher)

	// Start server in background
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("Server %s stopped: %v", collectorID, err)
		}
	}()

	return &testServer{
		dispatcher: dispatcher,
		grpcServer: grpcServer,
		listener:   listener,
		address:    address,
	}
}

// dialContext creates a client connection to the test server
func (ts *testServer) dialContext(ctx context.Context) (*grpc.ClientConn, error) {
	return grpc.DialContext(ctx, ts.address,
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return ts.listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

// shutdown stops the test server
func (ts *testServer) shutdown() {
	ts.dispatcher.Shutdown()
	ts.grpcServer.Stop()
	ts.listener.Close()
}

// TestConnection_BasicConnect tests basic connection establishment
func TestConnection_BasicConnect(t *testing.T) {
	ctx := context.Background()

	// Create two servers
	server1 := setupTestServer(t, "collector1", []string{"namespace1", "namespace2"})
	defer server1.shutdown()

	server2 := setupTestServer(t, "collector2", []string{"namespace2", "namespace3"})
	defer server2.shutdown()

	// Create client to server2
	conn, err := server2.dialContext(ctx)
	if err != nil {
		t.Fatalf("failed to dial server2: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)

	// Server1 connects to Server2
	req := &pb.ConnectRequest{
		Address:    server1.address,
		Namespaces: []string{"namespace1", "namespace2"},
		Metadata: map[string]string{
			"collector_id": "collector1",
		},
	}

	resp, err := client.Connect(ctx, req)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	if resp.ConnectionId == "" {
		t.Error("expected non-empty connection ID")
	}

	// Verify connection was recorded on server2
	connections := server2.dispatcher.GetConnectionManager().ListConnections()
	if len(connections) != 1 {
		t.Errorf("expected 1 connection on server2, got %d", len(connections))
	}

	if len(connections) > 0 {
		conn := connections[0]
		if conn.Id != resp.ConnectionId {
			t.Errorf("connection ID mismatch: %s != %s", conn.Id, resp.ConnectionId)
		}

		// Should have namespace2 as shared namespace
		if len(conn.SharedNamespaces) != 1 || conn.SharedNamespaces[0] != "namespace2" {
			t.Errorf("expected shared namespace 'namespace2', got %v", conn.SharedNamespaces)
		}
	}
}

// TestConnection_BidirectionalConnect tests bidirectional connection setup
func TestConnection_BidirectionalConnect(t *testing.T) {
	ctx := context.Background()

	// Create two servers
	server1 := setupTestServer(t, "collector1", []string{"ns1", "ns2"})
	defer server1.shutdown()

	server2 := setupTestServer(t, "collector2", []string{"ns2", "ns3"})
	defer server2.shutdown()

	// Server1 connects to Server2 using ConnectTo
	// We need to setup a real dial for this test
	server2Real := setupRealTestServer(t, "collector2-real", "localhost:0", []string{"ns2", "ns3"})
	defer server2Real.shutdown()

	// Get the actual address
	addr := server2Real.listener.Addr().String()

	// Server1 connects to Server2
	resp1, err := server1.dispatcher.ConnectTo(ctx, addr, []string{"ns1", "ns2"})
	if err != nil {
		t.Fatalf("Server1 ConnectTo failed: %v", err)
	}

	if resp1.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp1.Status.Code, resp1.Status.Message)
	}

	// Verify connection on server1
	conn1 := server1.dispatcher.GetConnectionManager().ListConnections()
	if len(conn1) != 1 {
		t.Errorf("expected 1 connection on server1, got %d", len(conn1))
	}

	// Verify connection on server2
	time.Sleep(100 * time.Millisecond) // Give time for connection to be established
	conn2 := server2Real.dispatcher.GetConnectionManager().ListConnections()
	if len(conn2) != 1 {
		t.Errorf("expected 1 connection on server2, got %d", len(conn2))
	}
}

// TestConnection_MultipleConnections tests multiple simultaneous connections
func TestConnection_MultipleConnections(t *testing.T) {
	ctx := context.Background()

	// Create hub server
	hub := setupTestServer(t, "hub", []string{"common"})
	defer hub.shutdown()

	// Create 3 peer servers
	peers := make([]*testServer, 3)
	for i := 0; i < 3; i++ {
		peers[i] = setupTestServer(t, fmt.Sprintf("peer%d", i), []string{"common"})
		defer peers[i].shutdown()
	}

	// Connect each peer to hub via the hub's dial context
	hubConn, err := hub.dialContext(ctx)
	if err != nil {
		t.Fatalf("failed to dial hub: %v", err)
	}
	defer hubConn.Close()

	hubClient := pb.NewCollectiveDispatcherClient(hubConn)

	for i, peer := range peers {
		req := &pb.ConnectRequest{
			Address:    peer.address,
			Namespaces: []string{"common"},
			Metadata: map[string]string{
				"peer_id": fmt.Sprintf("peer%d", i),
			},
		}

		resp, err := hubClient.Connect(ctx, req)
		if err != nil {
			t.Fatalf("peer%d Connect failed: %v", i, err)
		}

		if resp.Status.Code != 200 {
			t.Errorf("peer%d: expected status 200, got %d", i, resp.Status.Code)
		}
	}

	// Verify hub has 3 connections
	connections := hub.dispatcher.GetConnectionManager().ListConnections()
	if len(connections) != 3 {
		t.Errorf("expected 3 connections on hub, got %d", len(connections))
	}
}

// TestConnection_SharedNamespaces tests namespace sharing logic
func TestConnection_SharedNamespaces(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name              string
		server1Namespaces []string
		server2Namespaces []string
		expectedShared    []string
	}{
		{
			name:              "single shared namespace",
			server1Namespaces: []string{"ns1", "ns2"},
			server2Namespaces: []string{"ns2", "ns3"},
			expectedShared:    []string{"ns2"},
		},
		{
			name:              "multiple shared namespaces",
			server1Namespaces: []string{"ns1", "ns2", "ns3"},
			server2Namespaces: []string{"ns2", "ns3", "ns4"},
			expectedShared:    []string{"ns2", "ns3"},
		},
		{
			name:              "no shared namespaces",
			server1Namespaces: []string{"ns1"},
			server2Namespaces: []string{"ns2"},
			expectedShared:    []string{},
		},
		{
			name:              "all shared",
			server1Namespaces: []string{"ns1", "ns2"},
			server2Namespaces: []string{"ns1", "ns2"},
			expectedShared:    []string{"ns1", "ns2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server1 := setupTestServer(t, "s1", tt.server1Namespaces)
			defer server1.shutdown()

			server2 := setupTestServer(t, "s2", tt.server2Namespaces)
			defer server2.shutdown()

			// Connect server1 to server2
			conn, err := server2.dialContext(ctx)
			if err != nil {
				t.Fatalf("failed to dial: %v", err)
			}
			defer conn.Close()

			client := pb.NewCollectiveDispatcherClient(conn)
			req := &pb.ConnectRequest{
				Address:    server1.address,
				Namespaces: tt.server1Namespaces,
			}

			resp, err := client.Connect(ctx, req)
			if err != nil {
				t.Fatalf("Connect failed: %v", err)
			}

			if resp.Status.Code != 200 {
				t.Fatalf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
			}

			// Get the connection details
			connections := server2.dispatcher.GetConnectionManager().ListConnections()
			if len(connections) == 0 {
				t.Fatal("no connections found")
			}

			connection := connections[0]
			if len(connection.SharedNamespaces) != len(tt.expectedShared) {
				t.Errorf("expected %d shared namespaces, got %d: %v",
					len(tt.expectedShared), len(connection.SharedNamespaces), connection.SharedNamespaces)
			}
		})
	}
}

// TestConnection_InvalidRequests tests error handling
func TestConnection_InvalidRequests(t *testing.T) {
	ctx := context.Background()

	server := setupTestServer(t, "server", []string{"ns1"})
	defer server.shutdown()

	conn, err := server.dialContext(ctx)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)

	tests := []struct {
		name          string
		req           *pb.ConnectRequest
		expectedCode  pb.Status_Code
		expectedInMsg string
	}{
		{
			name: "empty address",
			req: &pb.ConnectRequest{
				Address:    "",
				Namespaces: []string{"ns1"},
			},
			expectedCode:  400,
			expectedInMsg: "address is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Connect(ctx, tt.req)
			if err != nil {
				t.Fatalf("Connect RPC failed: %v", err)
			}

			if resp.Status.Code != tt.expectedCode {
				t.Errorf("expected status %d, got %d", tt.expectedCode, resp.Status.Code)
			}
		})
	}
}

// realTestServer is for tests that need actual network connections
type realTestServer struct {
	dispatcher *dispatch.Dispatcher
	grpcServer *grpc.Server
	listener   net.Listener
	address    string
}

func setupRealTestServer(t *testing.T, collectorID, address string, namespaces []string) *realTestServer {
	t.Helper()

	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	dispatcher := dispatch.NewDispatcher(collectorID, listener.Addr().String(), namespaces)

	pb.RegisterCollectiveDispatcherServer(grpcServer, dispatcher)

	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("Server %s stopped: %v", collectorID, err)
		}
	}()

	return &realTestServer{
		dispatcher: dispatcher,
		grpcServer: grpcServer,
		listener:   listener,
		address:    listener.Addr().String(),
	}
}

func (rts *realTestServer) shutdown() {
	rts.dispatcher.Shutdown()
	rts.grpcServer.Stop()
	rts.listener.Close()
}

// TestConnection_RealNetwork tests connections over real network
func TestConnection_RealNetwork(t *testing.T) {
	ctx := context.Background()

	// Create two servers on random ports
	server1 := setupRealTestServer(t, "collector1", "localhost:0", []string{"ns1", "ns2"})
	defer server1.shutdown()

	server2 := setupRealTestServer(t, "collector2", "localhost:0", []string{"ns2", "ns3"})
	defer server2.shutdown()

	// Server1 connects to Server2
	resp, err := server1.dispatcher.ConnectTo(ctx, server2.address, []string{"ns1", "ns2"})
	if err != nil {
		t.Fatalf("ConnectTo failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	// Verify connections
	time.Sleep(100 * time.Millisecond)

	conn1 := server1.dispatcher.GetConnectionManager().ListConnections()
	if len(conn1) != 1 {
		t.Errorf("expected 1 connection on server1, got %d", len(conn1))
	}

	conn2 := server2.dispatcher.GetConnectionManager().ListConnections()
	if len(conn2) != 1 {
		t.Errorf("expected 1 connection on server2, got %d", len(conn2))
	}

	// Verify shared namespace
	if len(conn1) > 0 && len(conn2) > 0 {
		if len(conn1[0].SharedNamespaces) != 1 || conn1[0].SharedNamespaces[0] != "ns2" {
			t.Errorf("server1 expected shared namespace 'ns2', got %v", conn1[0].SharedNamespaces)
		}
		if len(conn2[0].SharedNamespaces) != 1 || conn2[0].SharedNamespaces[0] != "ns2" {
			t.Errorf("server2 expected shared namespace 'ns2', got %v", conn2[0].SharedNamespaces)
		}
	}

	// Now test that server2 can also initiate connection back
	// This creates a bidirectional connection setup
	resp2, err := server2.dispatcher.ConnectTo(ctx, server1.address, []string{"ns2", "ns3"})
	if err != nil {
		t.Fatalf("Server2 ConnectTo Server1 failed: %v", err)
	}

	if resp2.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp2.Status.Code, resp2.Status.Message)
	}

	time.Sleep(100 * time.Millisecond)

	// Now both should have 2 connections (one initiated by each)
	conn1 = server1.dispatcher.GetConnectionManager().ListConnections()
	conn2 = server2.dispatcher.GetConnectionManager().ListConnections()

	if len(conn1) != 2 {
		t.Errorf("expected 2 connections on server1, got %d", len(conn1))
	}

	if len(conn2) != 2 {
		t.Errorf("expected 2 connections on server2, got %d", len(conn2))
	}
}

// TestServe_BasicInvocation tests basic Serve RPC
func TestServe_BasicInvocation(t *testing.T) {
	ctx := context.Background()

	server := setupTestServer(t, "server1", []string{"test"})
	defer server.shutdown()

	// Register a simple handler
	server.dispatcher.RegisterService("test", "TestService", "Echo", func(ctx context.Context, input interface{}) (interface{}, error) {
		// Echo back the input
		return input, nil
	})

	// Create client
	conn, err := server.dialContext(ctx)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)

	// Create test input
	inputData, _ := anypb.New(&pb.Status{Code: 123, Message: "test"})

	req := &pb.ServeRequest{
		Namespace:  "test",
		Service:    &pb.ServiceTypeRef{ServiceName: "TestService"},
		MethodName: "Echo",
		Input:      inputData,
	}

	resp, err := client.Serve(ctx, req)
	if err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	if resp.ExecutorId != "server1" {
		t.Errorf("expected executor 'server1', got '%s'", resp.ExecutorId)
	}

	if resp.Output == nil {
		t.Error("expected output, got nil")
	}
}

// TestServe_InvalidRequests tests error handling for Serve
func TestServe_InvalidRequests(t *testing.T) {
	ctx := context.Background()

	server := setupTestServer(t, "server1", []string{"test"})
	defer server.shutdown()

	server.dispatcher.RegisterService("test", "TestService", "Method1", func(ctx context.Context, input interface{}) (interface{}, error) {
		return input, nil
	})

	conn, err := server.dialContext(ctx)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)
	inputData, _ := anypb.New(&pb.Status{Code: 123, Message: "test"})

	tests := []struct {
		name         string
		req          *pb.ServeRequest
		expectedCode pb.Status_Code
		expectedMsg  string
	}{
		{
			name: "empty namespace",
			req: &pb.ServeRequest{
				Namespace:  "",
				Service:    &pb.ServiceTypeRef{ServiceName: "TestService"},
				MethodName: "Method1",
				Input:      inputData,
			},
			expectedCode: 400,
			expectedMsg:  "namespace is required",
		},
		{
			name: "nil service",
			req: &pb.ServeRequest{
				Namespace:  "test",
				Service:    nil,
				MethodName: "Method1",
				Input:      inputData,
			},
			expectedCode: 400,
			expectedMsg:  "service is required",
		},
		{
			name: "empty service name",
			req: &pb.ServeRequest{
				Namespace:  "test",
				Service:    &pb.ServiceTypeRef{ServiceName: ""},
				MethodName: "Method1",
				Input:      inputData,
			},
			expectedCode: 400,
			expectedMsg:  "service is required",
		},
		{
			name: "empty method name",
			req: &pb.ServeRequest{
				Namespace:  "test",
				Service:    &pb.ServiceTypeRef{ServiceName: "TestService"},
				MethodName: "",
				Input:      inputData,
			},
			expectedCode: 400,
			expectedMsg:  "method_name is required",
		},
		{
			name: "namespace not found",
			req: &pb.ServeRequest{
				Namespace:  "nonexistent",
				Service:    &pb.ServiceTypeRef{ServiceName: "TestService"},
				MethodName: "Method1",
				Input:      inputData,
			},
			expectedCode: 404,
			expectedMsg:  "namespace 'nonexistent' not found",
		},
		{
			name: "method not found",
			req: &pb.ServeRequest{
				Namespace:  "test",
				Service:    &pb.ServiceTypeRef{ServiceName: "TestService"},
				MethodName: "NonexistentMethod",
				Input:      inputData,
			},
			expectedCode: 404,
			expectedMsg:  "method 'TestService.NonexistentMethod' not found in namespace 'test'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Serve(ctx, tt.req)
			if err != nil {
				t.Fatalf("Serve RPC failed: %v", err)
			}

			if resp.Status.Code != tt.expectedCode {
				t.Errorf("expected status %d, got %d: %s", tt.expectedCode, resp.Status.Code, resp.Status.Message)
			}

			if resp.Status.Message != tt.expectedMsg {
				t.Errorf("expected message '%s', got '%s'", tt.expectedMsg, resp.Status.Message)
			}
		})
	}
}

// TestServe_HandlerError tests error handling when handler returns error
func TestServe_HandlerError(t *testing.T) {
	ctx := context.Background()

	server := setupTestServer(t, "server1", []string{"test"})
	defer server.shutdown()

	// Register a handler that returns an error
	server.dispatcher.RegisterService("test", "TestService", "FailingMethod", func(ctx context.Context, input interface{}) (interface{}, error) {
		return nil, fmt.Errorf("intentional error")
	})

	conn, err := server.dialContext(ctx)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)
	inputData, _ := anypb.New(&pb.Status{Code: 123, Message: "test"})

	req := &pb.ServeRequest{
		Namespace:  "test",
		Service:    &pb.ServiceTypeRef{ServiceName: "TestService"},
		MethodName: "FailingMethod",
		Input:      inputData,
	}

	resp, err := client.Serve(ctx, req)
	if err != nil {
		t.Fatalf("Serve RPC failed: %v", err)
	}

	if resp.Status.Code != 500 {
		t.Errorf("expected status 500, got %d", resp.Status.Code)
	}

	if !strings.Contains(resp.Status.Message, "intentional error") {
		t.Errorf("expected error message to contain 'intentional error', got '%s'", resp.Status.Message)
	}
}

// TestServe_MultipleServices tests multiple services in same namespace
func TestServe_MultipleServices(t *testing.T) {
	ctx := context.Background()

	server := setupTestServer(t, "server1", []string{"test"})
	defer server.shutdown()

	// Register multiple services
	server.dispatcher.RegisterService("test", "ServiceA", "Method1", func(ctx context.Context, input interface{}) (interface{}, error) {
		return anypb.New(&pb.Status{Code: 1, Message: "ServiceA.Method1"})
	})

	server.dispatcher.RegisterService("test", "ServiceB", "Method1", func(ctx context.Context, input interface{}) (interface{}, error) {
		return anypb.New(&pb.Status{Code: 2, Message: "ServiceB.Method1"})
	})

	conn, err := server.dialContext(ctx)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)
	inputData, _ := anypb.New(&pb.Status{Code: 123, Message: "test"})

	// Call ServiceA
	resp1, err := client.Serve(ctx, &pb.ServeRequest{
		Namespace:  "test",
		Service:    &pb.ServiceTypeRef{ServiceName: "ServiceA"},
		MethodName: "Method1",
		Input:      inputData,
	})
	if err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	if resp1.Status.Code != 200 {
		t.Errorf("expected status 200, got %d", resp1.Status.Code)
	}

	// Call ServiceB
	resp2, err := client.Serve(ctx, &pb.ServeRequest{
		Namespace:  "test",
		Service:    &pb.ServiceTypeRef{ServiceName: "ServiceB"},
		MethodName: "Method1",
		Input:      inputData,
	})
	if err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	if resp2.Status.Code != 200 {
		t.Errorf("expected status 200, got %d", resp2.Status.Code)
	}

	// Verify outputs are different
	var output1, output2 pb.Status
	resp1.Output.UnmarshalTo(&output1)
	resp2.Output.UnmarshalTo(&output2)

	if output1.Message == output2.Message {
		t.Error("expected different outputs from different services")
	}
}

// TestDispatch_ToSpecificTarget tests dispatching to a specific collector
func TestDispatch_ToSpecificTarget(t *testing.T) {
	ctx := context.Background()

	// Create two servers
	server1 := setupRealTestServer(t, "collector1", "localhost:0", []string{"ns1"})
	defer server1.shutdown()

	server2 := setupRealTestServer(t, "collector2", "localhost:0", []string{"ns1"})
	defer server2.shutdown()

	// Register service on server2
	server2.dispatcher.RegisterService("ns1", "TestService", "Method1", func(ctx context.Context, input interface{}) (interface{}, error) {
		return anypb.New(&pb.Status{Code: 99, Message: "handled by server2"})
	})

	// Server1 connects to Server2
	_, err := server1.dispatcher.ConnectTo(ctx, server2.address, []string{"ns1"})
	if err != nil {
		t.Fatalf("ConnectTo failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create client to server1
	conn, err := grpc.NewClient(server1.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)
	inputData, _ := anypb.New(&pb.Status{Code: 123, Message: "test"})

	// Dispatch to server2
	req := &pb.DispatchRequest{
		Namespace:         "ns1",
		Service:           &pb.ServiceTypeRef{ServiceName: "TestService"},
		MethodName:        "Method1",
		Input:             inputData,
		TargetCollectorId: "collector2",
	}

	resp, err := client.Dispatch(ctx, req)
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	if resp.HandledByCollectorId != "collector2" {
		t.Errorf("expected handler 'collector2', got '%s'", resp.HandledByCollectorId)
	}
}

// TestDispatch_AutoRouteLocal tests auto-routing to local handler
func TestDispatch_AutoRouteLocal(t *testing.T) {
	ctx := context.Background()

	server := setupTestServer(t, "server1", []string{"test"})
	defer server.shutdown()

	// Register local service
	server.dispatcher.RegisterService("test", "LocalService", "Method1", func(ctx context.Context, input interface{}) (interface{}, error) {
		return anypb.New(&pb.Status{Code: 99, Message: "handled locally"})
	})

	conn, err := server.dialContext(ctx)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)
	inputData, _ := anypb.New(&pb.Status{Code: 123, Message: "test"})

	// Dispatch without target (should route locally)
	req := &pb.DispatchRequest{
		Namespace:  "test",
		Service:    &pb.ServiceTypeRef{ServiceName: "LocalService"},
		MethodName: "Method1",
		Input:      inputData,
	}

	resp, err := client.Dispatch(ctx, req)
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	if resp.HandledByCollectorId != "server1" {
		t.Errorf("expected handler 'server1', got '%s'", resp.HandledByCollectorId)
	}
}

// TestDispatch_AutoRouteRemote tests auto-routing to remote collector
func TestDispatch_AutoRouteRemote(t *testing.T) {
	ctx := context.Background()

	// Create two servers
	server1 := setupRealTestServer(t, "collector1", "localhost:0", []string{"ns1"})
	defer server1.shutdown()

	server2 := setupRealTestServer(t, "collector2", "localhost:0", []string{"ns1"})
	defer server2.shutdown()

	// Register service ONLY on server2
	server2.dispatcher.RegisterService("ns1", "RemoteService", "Method1", func(ctx context.Context, input interface{}) (interface{}, error) {
		return anypb.New(&pb.Status{Code: 99, Message: "handled by remote"})
	})

	// Server1 connects to Server2
	_, err := server1.dispatcher.ConnectTo(ctx, server2.address, []string{"ns1"})
	if err != nil {
		t.Fatalf("ConnectTo failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create client to server1
	conn, err := grpc.NewClient(server1.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)
	inputData, _ := anypb.New(&pb.Status{Code: 123, Message: "test"})

	// Dispatch without target (should auto-route to server2)
	req := &pb.DispatchRequest{
		Namespace:  "ns1",
		Service:    &pb.ServiceTypeRef{ServiceName: "RemoteService"},
		MethodName: "Method1",
		Input:      inputData,
	}

	resp, err := client.Dispatch(ctx, req)
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if resp.Status.Code != 200 {
		t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
	}

	if resp.HandledByCollectorId != "collector2" {
		t.Errorf("expected handler 'collector2', got '%s'", resp.HandledByCollectorId)
	}
}

// TestDispatch_InvalidRequests tests error handling for Dispatch
func TestDispatch_InvalidRequests(t *testing.T) {
	ctx := context.Background()

	server := setupTestServer(t, "server1", []string{"test"})
	defer server.shutdown()

	conn, err := server.dialContext(ctx)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)
	inputData, _ := anypb.New(&pb.Status{Code: 123, Message: "test"})

	tests := []struct {
		name         string
		req          *pb.DispatchRequest
		expectedCode pb.Status_Code
		expectedMsg  string
	}{
		{
			name: "empty namespace",
			req: &pb.DispatchRequest{
				Namespace:  "",
				Service:    &pb.ServiceTypeRef{ServiceName: "TestService"},
				MethodName: "Method1",
				Input:      inputData,
			},
			expectedCode: 400,
			expectedMsg:  "namespace is required",
		},
		{
			name: "nil service",
			req: &pb.DispatchRequest{
				Namespace:  "test",
				Service:    nil,
				MethodName: "Method1",
				Input:      inputData,
			},
			expectedCode: 400,
			expectedMsg:  "service is required",
		},
		{
			name: "empty method name",
			req: &pb.DispatchRequest{
				Namespace:  "test",
				Service:    &pb.ServiceTypeRef{ServiceName: "TestService"},
				MethodName: "",
				Input:      inputData,
			},
			expectedCode: 400,
			expectedMsg:  "method_name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Dispatch(ctx, tt.req)
			if err != nil {
				t.Fatalf("Dispatch RPC failed: %v", err)
			}

			if resp.Status.Code != tt.expectedCode {
				t.Errorf("expected status %d, got %d: %s", tt.expectedCode, resp.Status.Code, resp.Status.Message)
			}

			if resp.Status.Message != tt.expectedMsg {
				t.Errorf("expected message '%s', got '%s'", tt.expectedMsg, resp.Status.Message)
			}
		})
	}
}

// TestDispatch_TargetNotFound tests dispatching to non-existent target
func TestDispatch_TargetNotFound(t *testing.T) {
	ctx := context.Background()

	server := setupTestServer(t, "server1", []string{"test"})
	defer server.shutdown()

	conn, err := server.dialContext(ctx)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)
	inputData, _ := anypb.New(&pb.Status{Code: 123, Message: "test"})

	req := &pb.DispatchRequest{
		Namespace:         "test",
		Service:           &pb.ServiceTypeRef{ServiceName: "TestService"},
		MethodName:        "Method1",
		Input:             inputData,
		TargetCollectorId: "nonexistent",
	}

	resp, err := client.Dispatch(ctx, req)
	if err != nil {
		t.Fatalf("Dispatch RPC failed: %v", err)
	}

	if resp.Status.Code != 404 {
		t.Errorf("expected status 404, got %d", resp.Status.Code)
	}

	if !strings.Contains(resp.Status.Message, "no connection to collector") {
		t.Errorf("expected 'no connection' message, got '%s'", resp.Status.Message)
	}
}

// TestDispatch_NoCollectorForNamespace tests when no collector handles the namespace
func TestDispatch_NoCollectorForNamespace(t *testing.T) {
	ctx := context.Background()

	server := setupTestServer(t, "server1", []string{"ns1"})
	defer server.shutdown()

	conn, err := server.dialContext(ctx)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectiveDispatcherClient(conn)
	inputData, _ := anypb.New(&pb.Status{Code: 123, Message: "test"})

	// Try to dispatch to a namespace we don't have
	req := &pb.DispatchRequest{
		Namespace:  "nonexistent",
		Service:    &pb.ServiceTypeRef{ServiceName: "TestService"},
		MethodName: "Method1",
		Input:      inputData,
	}

	resp, err := client.Dispatch(ctx, req)
	if err != nil {
		t.Fatalf("Dispatch RPC failed: %v", err)
	}

	if resp.Status.Code != 404 {
		t.Errorf("expected status 404, got %d", resp.Status.Code)
	}

	if !strings.Contains(resp.Status.Message, "no collector found for namespace") {
		t.Errorf("expected 'no collector found' message, got '%s'", resp.Status.Message)
	}
}
