package registry

import (
	"context"
	"testing"

	"github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestValidationInterceptor(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Register a service
	req := &collector.RegisterServiceRequest{
		Namespace: "test",
		ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
			Name: proto.String("TestService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: proto.String("TestMethod")},
				{Name: proto.String("AnotherMethod")},
			},
		},
	}

	_, err := server.RegisterService(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	// Create interceptor
	interceptor := server.ValidationInterceptor("test")

	// Test valid method
	t.Run("ValidMethod", func(t *testing.T) {
		called := false
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			called = true
			return "success", nil
		}

		info := &grpc.UnaryServerInfo{
			FullMethod: "/collector.TestService/TestMethod",
		}

		resp, err := interceptor(context.Background(), nil, info, handler)
		if err != nil {
			t.Errorf("interceptor returned error for valid method: %v", err)
		}
		if !called {
			t.Error("handler was not called for valid method")
		}
		if resp != "success" {
			t.Errorf("expected response 'success', got %v", resp)
		}
	})

	// Test invalid method
	t.Run("InvalidMethod", func(t *testing.T) {
		called := false
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			called = true
			return "success", nil
		}

		info := &grpc.UnaryServerInfo{
			FullMethod: "/collector.TestService/NonexistentMethod",
		}

		_, err := interceptor(context.Background(), nil, info, handler)
		if err == nil {
			t.Error("interceptor should return error for invalid method")
		}
		if called {
			t.Error("handler should not be called for invalid method")
		}
		if status.Code(err) != codes.Unimplemented {
			t.Errorf("expected status code %v, got %v", codes.Unimplemented, status.Code(err))
		}
	})

	// Test invalid service
	t.Run("InvalidService", func(t *testing.T) {
		called := false
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			called = true
			return "success", nil
		}

		info := &grpc.UnaryServerInfo{
			FullMethod: "/collector.NonexistentService/TestMethod",
		}

		_, err := interceptor(context.Background(), nil, info, handler)
		if err == nil {
			t.Error("interceptor should return error for invalid service")
		}
		if called {
			t.Error("handler should not be called for invalid service")
		}
		if status.Code(err) != codes.Unimplemented {
			t.Errorf("expected status code %v, got %v", codes.Unimplemented, status.Code(err))
		}
	})

	// Test malformed method name
	t.Run("MalformedMethodName", func(t *testing.T) {
		called := false
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			called = true
			return "success", nil
		}

		info := &grpc.UnaryServerInfo{
			FullMethod: "InvalidFormat",
		}

		// Should skip validation for malformed names
		_, err := interceptor(context.Background(), nil, info, handler)
		if err != nil {
			t.Errorf("interceptor should skip validation for malformed names: %v", err)
		}
		if !called {
			t.Error("handler should be called when validation is skipped")
		}
	})

	// Test another valid method on same service
	t.Run("AnotherValidMethod", func(t *testing.T) {
		called := false
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			called = true
			return "success", nil
		}

		info := &grpc.UnaryServerInfo{
			FullMethod: "/collector.TestService/AnotherMethod",
		}

		resp, err := interceptor(context.Background(), nil, info, handler)
		if err != nil {
			t.Errorf("interceptor returned error for valid method: %v", err)
		}
		if !called {
			t.Error("handler was not called for valid method")
		}
		if resp != "success" {
			t.Errorf("expected response 'success', got %v", resp)
		}
	})
}

func TestStreamValidationInterceptor(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Register a service with streaming method
	req := &collector.RegisterServiceRequest{
		Namespace: "test",
		ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
			Name: proto.String("StreamService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{
					Name:            proto.String("StreamMethod"),
					ServerStreaming: proto.Bool(true),
				},
			},
		},
	}

	_, err := server.RegisterService(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	// Create interceptor
	interceptor := server.StreamValidationInterceptor("test")

	// Test valid stream method
	t.Run("ValidStreamMethod", func(t *testing.T) {
		called := false
		handler := func(srv interface{}, stream grpc.ServerStream) error {
			called = true
			return nil
		}

		info := &grpc.StreamServerInfo{
			FullMethod: "/collector.StreamService/StreamMethod",
		}

		mockStream := &mockServerStream{ctx: context.Background()}
		err := interceptor(nil, mockStream, info, handler)
		if err != nil {
			t.Errorf("interceptor returned error for valid stream method: %v", err)
		}
		if !called {
			t.Error("handler was not called for valid stream method")
		}
	})

	// Test invalid stream method
	t.Run("InvalidStreamMethod", func(t *testing.T) {
		called := false
		handler := func(srv interface{}, stream grpc.ServerStream) error {
			called = true
			return nil
		}

		info := &grpc.StreamServerInfo{
			FullMethod: "/collector.StreamService/NonexistentMethod",
		}

		mockStream := &mockServerStream{ctx: context.Background()}
		err := interceptor(nil, mockStream, info, handler)
		if err == nil {
			t.Error("interceptor should return error for invalid stream method")
		}
		if called {
			t.Error("handler should not be called for invalid stream method")
		}
		if status.Code(err) != codes.Unimplemented {
			t.Errorf("expected status code %v, got %v", codes.Unimplemented, status.Code(err))
		}
	})
}

// Mock ServerStream for testing
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}
