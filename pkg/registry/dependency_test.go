package registry

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TestRegisterProto_MissingDependency tests that registering a proto with missing dependencies fails
func TestRegisterProto_MissingDependency(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Try to register a proto that depends on a non-existent proto
	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("dependent.proto"),
			Dependency: []string{
				"missing.proto", // Doesn't exist and not a well-known type
			},
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("MyMessage")},
			},
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if err == nil {
		t.Error("Expected error when registering proto with missing dependency, got nil")
	}

	// Should be InvalidArgument error
	st, ok := status.FromError(err)
	if !ok {
		t.Errorf("Expected gRPC status error, got %T", err)
	} else if st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error code, got %v", st.Code())
	}

	// Error message should mention the missing dependency
	if err != nil && !strings.Contains(err.Error(), "missing.proto") {
		t.Errorf("Error message should mention missing dependency 'missing.proto', got: %v", err)
	}
}

// TestRegisterProto_MultipleMissingDependencies tests multiple missing dependencies
func TestRegisterProto_MultipleMissingDependencies(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("dependent.proto"),
			Dependency: []string{
				"missing1.proto",
				"missing2.proto",
				"missing3.proto",
			},
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("MyMessage")},
			},
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error when registering proto with missing dependencies")
	}

	// Error should mention all missing dependencies
	errMsg := err.Error()
	if !strings.Contains(errMsg, "missing1.proto") {
		t.Error("Error should mention missing1.proto")
	}
	if !strings.Contains(errMsg, "missing2.proto") {
		t.Error("Error should mention missing2.proto")
	}
	if !strings.Contains(errMsg, "missing3.proto") {
		t.Error("Error should mention missing3.proto")
	}
}

// TestRegisterProto_WithValidDependencies tests that registration succeeds with valid dependencies
func TestRegisterProto_WithValidDependencies(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// First, register the dependency
	depReq := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("base.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("BaseMessage")},
			},
		},
	}
	_, err := server.RegisterProto(context.Background(), depReq)
	if err != nil {
		t.Fatalf("Failed to register base proto: %v", err)
	}

	// Now register a proto that depends on it
	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("dependent.proto"),
			Dependency: []string{
				"base.proto", // This now exists
			},
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("DependentMessage")},
			},
		},
	}

	_, err = server.RegisterProto(context.Background(), req)
	if err != nil {
		t.Errorf("Expected success when dependencies exist, got error: %v", err)
	}
}

// TestRegisterProto_CircularDependency tests detection of circular dependencies
func TestRegisterProto_CircularDependency(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Register A.proto
	reqA := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("a.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("MessageA")},
			},
		},
	}
	_, err := server.RegisterProto(context.Background(), reqA)
	if err != nil {
		t.Fatalf("Failed to register a.proto: %v", err)
	}

	// Register B.proto that depends on A
	reqB := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name:       proto.String("b.proto"),
			Dependency: []string{"a.proto"},
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("MessageB")},
			},
		},
	}
	_, err = server.RegisterProto(context.Background(), reqB)
	if err != nil {
		t.Fatalf("Failed to register b.proto: %v", err)
	}

	// Try to register C.proto that depends on B but creates circular dependency
	// by making A depend on C (would need to update A, but let's test direct circle)
	reqC := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name:       proto.String("c.proto"),
			Dependency: []string{"b.proto"},
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("MessageC")},
			},
		},
	}
	_, err = server.RegisterProto(context.Background(), reqC)
	if err != nil {
		t.Fatalf("Failed to register c.proto: %v", err)
	}

	// Now try to update A to depend on C (creating a cycle: A -> C -> B -> A)
	// Since we don't have Update yet, let's test self-reference
	reqSelf := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name:       proto.String("self.proto"),
			Dependency: []string{"self.proto"}, // Self-reference
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("SelfMessage")},
			},
		},
	}

	_, err = server.RegisterProto(context.Background(), reqSelf)
	if err == nil {
		t.Error("Expected error when proto depends on itself")
	}

	if err != nil && !strings.Contains(err.Error(), "circular") {
		t.Errorf("Error should mention circular dependency, got: %v", err)
	}
}

// TestRegisterProto_TransitiveDependencies tests that transitive dependencies are validated
func TestRegisterProto_TransitiveDependencies(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Register A.proto
	reqA := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("a.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("MessageA")},
			},
		},
	}
	_, err := server.RegisterProto(context.Background(), reqA)
	if err != nil {
		t.Fatalf("Failed to register a.proto: %v", err)
	}

	// Register B.proto that depends on A
	reqB := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name:       proto.String("b.proto"),
			Dependency: []string{"a.proto"},
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("MessageB")},
			},
		},
	}
	_, err = server.RegisterProto(context.Background(), reqB)
	if err != nil {
		t.Fatalf("Failed to register b.proto: %v", err)
	}

	// Try to register C.proto that depends on B and missing.proto
	// This tests that we validate transitive deps exist
	reqC := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("c.proto"),
			Dependency: []string{
				"b.proto",       // Exists
				"missing.proto", // Doesn't exist
			},
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("MessageC")},
			},
		},
	}

	_, err = server.RegisterProto(context.Background(), reqC)
	if err == nil {
		t.Error("Expected error when transitive dependency is missing")
	}

	if err != nil && !strings.Contains(err.Error(), "missing.proto") {
		t.Errorf("Error should mention missing dependency, got: %v", err)
	}
}

// TestRegisterProto_CrossNamespaceDependency tests dependencies across namespaces
func TestRegisterProto_CrossNamespaceDependency(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Register proto in namespace1
	req1 := &collector.RegisterProtoRequest{
		Namespace: "namespace1",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("common.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("CommonMessage")},
			},
		},
	}
	_, err := server.RegisterProto(context.Background(), req1)
	if err != nil {
		t.Fatalf("Failed to register in namespace1: %v", err)
	}

	// Try to register in namespace2 with dependency on namespace1's proto
	// This should FAIL because namespace1 and namespace2 are different branches
	req2 := &collector.RegisterProtoRequest{
		Namespace: "namespace2",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("app.proto"),
			Dependency: []string{
				"common.proto", // Exists in namespace1, not in namespace2's hierarchy
			},
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("AppMessage")},
			},
		},
	}

	_, err = server.RegisterProto(context.Background(), req2)
	if err == nil {
		t.Error("Expected error when depending on proto in different namespace branch")
	}

	if err != nil && !strings.Contains(err.Error(), "missing") {
		t.Errorf("Error should mention missing dependency, got: %v", err)
	}
}

// TestRegisterProto_WellKnownTypes tests that well-known protobuf types don't need to be registered
func TestRegisterProto_WellKnownTypes(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// These should be allowed without registration
	wellKnownTypes := []string{
		// Google protobuf types
		"google/protobuf/any.proto",
		"google/protobuf/timestamp.proto",
		"google/protobuf/duration.proto",
		"google/protobuf/empty.proto",
		"google/protobuf/wrappers.proto",
		"google/protobuf/struct.proto",
		"google/protobuf/field_mask.proto",
		// Collector core types
		"collection.proto",
		"registry.proto",
	}

	for i, wkt := range wellKnownTypes {
		req := &collector.RegisterProtoRequest{
			Namespace: "test",
			FileDescriptor: &descriptorpb.FileDescriptorProto{
				Name:       proto.String(fmt.Sprintf("myapp_%d.proto", i)), // Use unique names
				Dependency: []string{wkt},
				MessageType: []*descriptorpb.DescriptorProto{
					{Name: proto.String("MyMessage")},
				},
			},
		}

		_, err := server.RegisterProto(context.Background(), req)
		if err != nil {
			t.Errorf("Well-known type %s should not require registration, got error: %v", wkt, err)
		}
	}
}

// TestRegisterProto_EmptyDependencies tests that protos with no dependencies work
func TestRegisterProto_EmptyDependencies(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name:       proto.String("standalone.proto"),
			Dependency: []string{}, // Explicitly empty
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("StandaloneMessage")},
			},
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if err != nil {
		t.Errorf("Proto with empty dependencies should succeed, got: %v", err)
	}
}

// TestRegisterProto_HierarchicalNamespacesNotSupported tests that hierarchical namespaces (with slashes) are rejected
func TestRegisterProto_HierarchicalNamespacesNotSupported(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Register a common proto in a flat namespace first
	parentReq := &collector.RegisterProtoRequest{
		Namespace: "team",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("common.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("CommonMessage")},
			},
		},
	}
	_, err := server.RegisterProto(context.Background(), parentReq)
	if err != nil {
		t.Fatalf("Failed to register in flat namespace: %v", err)
	}

	// Hierarchical namespaces with "/" are NOT supported - they should be rejected
	childReq := &collector.RegisterProtoRequest{
		Namespace: "team/project",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("app.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("AppMessage")},
			},
		},
	}

	_, err = server.RegisterProto(context.Background(), childReq)
	if err == nil {
		t.Error("Expected error when using hierarchical namespace with slash, got nil")
	}

	// Deeper hierarchical namespace should also be rejected
	deepChildReq := &collector.RegisterProtoRequest{
		Namespace: "team/project/service",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("service.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("ServiceMessage")},
			},
		},
	}

	_, err = server.RegisterProto(context.Background(), deepChildReq)
	if err == nil {
		t.Error("Expected error when using deep hierarchical namespace with slashes, got nil")
	}
}
