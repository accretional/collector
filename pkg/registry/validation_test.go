package registry

import (
	"context"
	"strings"
	"testing"

	"github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TestRegisterProto_ReservedNamespace tests that reserved namespaces are rejected
func TestRegisterProto_ReservedNamespace(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Only filesystem-conflicting namespaces are reserved
	// "system", "internal", "admin", "metadata" are intentionally NOT reserved
	// as they are used for system collections
	reservedNamespaces := []string{
		"repo",
		"backups",
		"files",
		// Note: .backup is excluded because it's already blocked by the "no leading dot" rule
	}

	for _, ns := range reservedNamespaces {
		req := &collector.RegisterProtoRequest{
			Namespace: ns,
			FileDescriptor: &descriptorpb.FileDescriptorProto{
				Name: proto.String("test.proto"),
				MessageType: []*descriptorpb.DescriptorProto{
					{Name: proto.String("TestMessage")},
				},
			},
		}

		_, err := server.RegisterProto(context.Background(), req)
		if err == nil {
			t.Errorf("Expected error when registering in reserved namespace %s, got nil", ns)
		}

		st, ok := status.FromError(err)
		if !ok {
			t.Errorf("Expected gRPC status error, got %T", err)
		} else if st.Code() != codes.InvalidArgument {
			t.Errorf("Expected InvalidArgument error code for namespace %s, got %v", ns, st.Code())
		}

		if err != nil && !strings.Contains(err.Error(), "reserved") {
			t.Errorf("Error should mention 'reserved' for namespace %s, got: %v", ns, err)
		}
	}
}

// TestRegisterProto_InvalidNamespaceCharacters tests rejection of invalid characters
func TestRegisterProto_InvalidNamespaceCharacters(t *testing.T) {
	server, _, _ := setupTestServer(t)

	invalidNamespaces := []string{
		"../test",           // Path traversal
		"../../etc/passwd",  // Path traversal
		"test/../malicious", // Path traversal in middle
		".hidden",           // Starts with dot
		"test..name",        // Contains ..
		"test\\name",        // Backslash
		"test:name",         // Colon
		"test*name",         // Asterisk
		"test?name",         // Question mark
		"test\"name",        // Quote
		"test<name",         // Less than
		"test>name",         // Greater than
		"test|name",         // Pipe
	}

	for _, ns := range invalidNamespaces {
		req := &collector.RegisterProtoRequest{
			Namespace: ns,
			FileDescriptor: &descriptorpb.FileDescriptorProto{
				Name: proto.String("test.proto"),
				MessageType: []*descriptorpb.DescriptorProto{
					{Name: proto.String("TestMessage")},
				},
			},
		}

		_, err := server.RegisterProto(context.Background(), req)
		if err == nil {
			t.Errorf("Expected error when registering with invalid namespace %q, got nil", ns)
		}

		st, ok := status.FromError(err)
		if !ok {
			t.Errorf("Expected gRPC status error for namespace %q, got %T", ns, err)
		} else if st.Code() != codes.InvalidArgument {
			t.Errorf("Expected InvalidArgument error code for namespace %q, got %v", ns, st.Code())
		}
	}
}

// TestRegisterProto_InvalidProtoName tests rejection of invalid proto names
func TestRegisterProto_InvalidProtoName(t *testing.T) {
	server, _, _ := setupTestServer(t)

	invalidNames := []string{
		"../test.proto",     // Path traversal
		"../../etc/passwd",  // Path traversal
		"test/../bad.proto", // Path traversal in middle
		".hidden.proto",     // Starts with dot
		"test..proto",       // Contains ..
		"test\\bad.proto",   // Backslash
		"test:bad.proto",    // Colon
		"test*bad.proto",    // Asterisk
		"test?bad.proto",    // Question mark
		"test\"bad.proto",   // Quote
		"test<bad.proto",    // Less than
		"test>bad.proto",    // Greater than
		"test|bad.proto",    // Pipe
		"test/bad.proto",    // Slash (breaks ID format)
	}

	for _, name := range invalidNames {
		req := &collector.RegisterProtoRequest{
			Namespace: "test",
			FileDescriptor: &descriptorpb.FileDescriptorProto{
				Name: proto.String(name),
				MessageType: []*descriptorpb.DescriptorProto{
					{Name: proto.String("TestMessage")},
				},
			},
		}

		_, err := server.RegisterProto(context.Background(), req)
		if err == nil {
			t.Errorf("Expected error when registering proto with invalid name %q, got nil", name)
		}

		st, ok := status.FromError(err)
		if !ok {
			t.Errorf("Expected gRPC status error for name %q, got %T", name, err)
		} else if st.Code() != codes.InvalidArgument {
			t.Errorf("Expected InvalidArgument error code for name %q, got %v", name, st.Code())
		}
	}
}

// TestRegisterProto_NamespaceTooLong tests rejection of extremely long namespaces
func TestRegisterProto_NamespaceTooLong(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Create a namespace longer than 255 characters
	longNamespace := strings.Repeat("a", 256)

	req := &collector.RegisterProtoRequest{
		Namespace: longNamespace,
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("test.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("TestMessage")},
			},
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if err == nil {
		t.Error("Expected error when registering with namespace longer than 255 chars, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Errorf("Expected gRPC status error, got %T", err)
	} else if st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error code, got %v", st.Code())
	}
}

// TestRegisterProto_ProtoNameTooLong tests rejection of extremely long proto names
func TestRegisterProto_ProtoNameTooLong(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Create a proto name longer than 255 characters
	longName := strings.Repeat("a", 256) + ".proto"

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String(longName),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("TestMessage")},
			},
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if err == nil {
		t.Error("Expected error when registering proto with name longer than 255 chars, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Errorf("Expected gRPC status error, got %T", err)
	} else if st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error code, got %v", st.Code())
	}
}

// TestRegisterService_ReservedNamespace tests that reserved namespaces are rejected for services
func TestRegisterService_ReservedNamespace(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := &collector.RegisterServiceRequest{
		Namespace: "repo",
		ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
			Name: proto.String("TestService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: proto.String("TestMethod")},
			},
		},
	}

	_, err := server.RegisterService(context.Background(), req)
	if err == nil {
		t.Error("Expected error when registering service in reserved namespace, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Errorf("Expected gRPC status error, got %T", err)
	} else if st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error code, got %v", st.Code())
	}
}

// TestRegisterService_InvalidServiceName tests rejection of invalid service names
func TestRegisterService_InvalidServiceName(t *testing.T) {
	server, _, _ := setupTestServer(t)

	invalidNames := []string{
		"../TestService",
		"Test/../Service",
		".HiddenService",
		"Test..Service",
		"Test/Service", // Slash breaks ID format
	}

	for _, name := range invalidNames {
		req := &collector.RegisterServiceRequest{
			Namespace: "test",
			ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
				Name: proto.String(name),
				Method: []*descriptorpb.MethodDescriptorProto{
					{Name: proto.String("TestMethod")},
				},
			},
		}

		_, err := server.RegisterService(context.Background(), req)
		if err == nil {
			t.Errorf("Expected error when registering service with invalid name %q, got nil", name)
		}

		st, ok := status.FromError(err)
		if !ok {
			t.Errorf("Expected gRPC status error for name %q, got %T", name, err)
		} else if st.Code() != codes.InvalidArgument {
			t.Errorf("Expected InvalidArgument error code for name %q, got %v", name, st.Code())
		}
	}
}

// TestRegisterProto_SlashInNamespaceRejected tests that forward slashes in namespaces are rejected
// (hierarchical namespaces are not supported)
func TestRegisterProto_SlashInNamespaceRejected(t *testing.T) {
	server, _, _ := setupTestServer(t)

	invalidNamespaces := []string{
		"team/project",
		"team/project/service",
		"org/dept/team/project",
	}

	for _, ns := range invalidNamespaces {
		req := &collector.RegisterProtoRequest{
			Namespace: ns,
			FileDescriptor: &descriptorpb.FileDescriptorProto{
				Name: proto.String("test.proto"),
				MessageType: []*descriptorpb.DescriptorProto{
					{Name: proto.String("TestMessage")},
				},
			},
		}

		_, err := server.RegisterProto(context.Background(), req)
		if err == nil {
			t.Errorf("Expected error for namespace with slash %q, got nil", ns)
		}
	}
}
