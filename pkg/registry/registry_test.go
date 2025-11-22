package registry

import (
	"context"
	"os"
	"testing"

	"github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func setupTestServer(t *testing.T) (*RegistryServer, *collection.Collection, *collection.Collection) {
	registeredProtos, err := collection.NewCollection(&collector.Collection{Namespace: "system", Name: "registered_protos"}, newTempStore(t), &collection.LocalFileSystem{})
	if err != nil {
		t.Fatalf("failed to create registered protos collection: %v", err)
	}

	registeredServices, err := collection.NewCollection(&collector.Collection{Namespace: "system", Name: "registered_services"}, newTempStore(t), &collection.LocalFileSystem{})
	if err != nil {
		t.Fatalf("failed to create registered services collection: %v", err)
	}

	server := NewRegistryServer(registeredProtos, registeredServices)
	return server, registeredProtos, registeredServices
}

func newTempStore(t *testing.T) collection.Store {
	f, err := os.CreateTemp("", "test.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	t.Cleanup(func() {
		os.Remove(f.Name())
	})

	store, err := sqlite.NewSqliteStore(f.Name(), collection.Options{EnableJSON: true})
	if err != nil {
		t.Fatalf("failed to create in-memory store: %v", err)
	}
	return store
}

func TestRegisterProto(t *testing.T) {
	server, registeredProtos, _ := setupTestServer(t)

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("test.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: proto.String("TestMessage"),
				},
			},
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterProto failed: %v", err)
	}

	record, err := registeredProtos.GetRecord(context.Background(), "test/test.proto")
	if err != nil {
		t.Fatalf("GetRecord failed: %v", err)
	}

	registeredProto := &collector.RegisteredProto{}
	err = proto.Unmarshal(record.ProtoData, registeredProto)
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if registeredProto.Namespace != "test" {
		t.Errorf("expected namespace 'test', got '%s'", registeredProto.Namespace)
	}

	if len(registeredProto.MessageNames) != 1 || registeredProto.MessageNames[0] != "TestMessage" {
		t.Errorf("expected message name 'TestMessage', got '%v'", registeredProto.MessageNames)
	}
}

func TestRegisterService(t *testing.T) {
	server, _, registeredServices := setupTestServer(t)

	req := &collector.RegisterServiceRequest{
		Namespace: "test",
		ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
			Name: proto.String("TestService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{
					Name: proto.String("TestMethod"),
				},
			},
		},
	}

	_, err := server.RegisterService(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	record, err := registeredServices.GetRecord(context.Background(), "test/TestService")
	if err != nil {
		t.Fatalf("GetRecord failed: %v", err)
	}

	registeredService := &collector.RegisteredService{}
	err = proto.Unmarshal(record.ProtoData, registeredService)
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if registeredService.Namespace != "test" {
		t.Errorf("expected namespace 'test', got '%s'", registeredService.Namespace)
	}

	if registeredService.ServiceName != "TestService" {
		t.Errorf("expected service name 'TestService', got '%s'", registeredService.ServiceName)
	}

	if len(registeredService.MethodNames) != 1 || registeredService.MethodNames[0] != "TestMethod" {
		t.Errorf("expected method name 'TestMethod', got '%v'", registeredService.MethodNames)
	}
}

func TestRegisterProto_Duplicate(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("test.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: proto.String("TestMessage"),
				},
			},
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterProto failed: %v", err)
	}

	_, err = server.RegisterProto(context.Background(), req)
	if status.Code(err) != codes.AlreadyExists {
		t.Errorf("expected status code %v, got %v", codes.AlreadyExists, status.Code(err))
	}
}

func TestRegisterService_Duplicate(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := &collector.RegisterServiceRequest{
		Namespace: "test",
		ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
			Name: proto.String("TestService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{
					Name: proto.String("TestMethod"),
				},
			},
		},
	}

	_, err := server.RegisterService(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	_, err = server.RegisterService(context.Background(), req)
	if status.Code(err) != codes.AlreadyExists {
		t.Errorf("expected status code %v, got %v", codes.AlreadyExists, status.Code(err))
	}
}

func TestRegisterProto_NilName(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: nil,
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: proto.String("TestMessage"),
				},
			},
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected status code %v, got %v", codes.InvalidArgument, status.Code(err))
	}
}

func TestRegisterService_NilName(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := &collector.RegisterServiceRequest{
		Namespace: "test",
		ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
			Name: nil,
			Method: []*descriptorpb.MethodDescriptorProto{
				{
					Name: proto.String("TestMethod"),
				},
			},
		},
	}

	_, err := server.RegisterService(context.Background(), req)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected status code %v, got %v", codes.InvalidArgument, status.Code(err))
	}
}

// TestRegisterProto_EmptyNamespace tests empty namespace validation
func TestRegisterProto_EmptyNamespace(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := &collector.RegisterProtoRequest{
		Namespace: "",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("test.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: proto.String("TestMessage"),
				},
			},
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected status code %v, got %v", codes.InvalidArgument, status.Code(err))
	}
}

// TestRegisterService_EmptyNamespace tests empty namespace validation
func TestRegisterService_EmptyNamespace(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := &collector.RegisterServiceRequest{
		Namespace: "",
		ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
			Name: proto.String("TestService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{
					Name: proto.String("TestMethod"),
				},
			},
		},
	}

	_, err := server.RegisterService(context.Background(), req)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected status code %v, got %v", codes.InvalidArgument, status.Code(err))
	}
}

// TestRegisterProto_MultipleNamespaces tests same proto in different namespaces
func TestRegisterProto_MultipleNamespaces(t *testing.T) {
	server, registeredProtos, _ := setupTestServer(t)

	descriptor := &descriptorpb.FileDescriptorProto{
		Name: proto.String("common.proto"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("CommonMessage"),
			},
		},
	}

	// Register in namespace1
	resp1, err := server.RegisterProto(context.Background(), &collector.RegisterProtoRequest{
		Namespace:      "namespace1",
		FileDescriptor: descriptor,
	})
	if err != nil {
		t.Fatalf("RegisterProto failed for namespace1: %v", err)
	}

	if resp1.ProtoId != "namespace1/common.proto" {
		t.Errorf("expected proto ID 'namespace1/common.proto', got '%s'", resp1.ProtoId)
	}

	// Register same proto in namespace2 - should succeed
	resp2, err := server.RegisterProto(context.Background(), &collector.RegisterProtoRequest{
		Namespace:      "namespace2",
		FileDescriptor: descriptor,
	})
	if err != nil {
		t.Fatalf("RegisterProto failed for namespace2: %v", err)
	}

	if resp2.ProtoId != "namespace2/common.proto" {
		t.Errorf("expected proto ID 'namespace2/common.proto', got '%s'", resp2.ProtoId)
	}

	// Verify both exist independently
	_, err = registeredProtos.GetRecord(context.Background(), "namespace1/common.proto")
	if err != nil {
		t.Errorf("failed to get namespace1 proto: %v", err)
	}

	_, err = registeredProtos.GetRecord(context.Background(), "namespace2/common.proto")
	if err != nil {
		t.Errorf("failed to get namespace2 proto: %v", err)
	}
}

// TestRegisterService_MultipleNamespaces tests same service in different namespaces
func TestRegisterService_MultipleNamespaces(t *testing.T) {
	server, _, registeredServices := setupTestServer(t)

	descriptor := &descriptorpb.ServiceDescriptorProto{
		Name: proto.String("CommonService"),
		Method: []*descriptorpb.MethodDescriptorProto{
			{
				Name: proto.String("CommonMethod"),
			},
		},
	}

	// Register in namespace1
	resp1, err := server.RegisterService(context.Background(), &collector.RegisterServiceRequest{
		Namespace:         "namespace1",
		ServiceDescriptor: descriptor,
	})
	if err != nil {
		t.Fatalf("RegisterService failed for namespace1: %v", err)
	}

	if resp1.ServiceId != "namespace1/CommonService" {
		t.Errorf("expected service ID 'namespace1/CommonService', got '%s'", resp1.ServiceId)
	}

	// Register same service in namespace2 - should succeed
	resp2, err := server.RegisterService(context.Background(), &collector.RegisterServiceRequest{
		Namespace:         "namespace2",
		ServiceDescriptor: descriptor,
	})
	if err != nil {
		t.Fatalf("RegisterService failed for namespace2: %v", err)
	}

	if resp2.ServiceId != "namespace2/CommonService" {
		t.Errorf("expected service ID 'namespace2/CommonService', got '%s'", resp2.ServiceId)
	}

	// Verify both exist independently
	_, err = registeredServices.GetRecord(context.Background(), "namespace1/CommonService")
	if err != nil {
		t.Errorf("failed to get namespace1 service: %v", err)
	}

	_, err = registeredServices.GetRecord(context.Background(), "namespace2/CommonService")
	if err != nil {
		t.Errorf("failed to get namespace2 service: %v", err)
	}
}

// TestRegisterProto_WithDependencies tests dependencies field
func TestRegisterProto_WithDependencies(t *testing.T) {
	server, registeredProtos, _ := setupTestServer(t)

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name:       proto.String("dependent.proto"),
			Dependency: []string{"google/protobuf/timestamp.proto", "common.proto"},
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: proto.String("DependentMessage"),
				},
			},
		},
	}

	resp, err := server.RegisterProto(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterProto failed: %v", err)
	}

	// Retrieve and verify dependencies
	record, err := registeredProtos.GetRecord(context.Background(), resp.ProtoId)
	if err != nil {
		t.Fatalf("GetRecord failed: %v", err)
	}

	registeredProto := &collector.RegisteredProto{}
	err = proto.Unmarshal(record.ProtoData, registeredProto)
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(registeredProto.Dependencies) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(registeredProto.Dependencies))
	}

	expectedDeps := []string{"google/protobuf/timestamp.proto", "common.proto"}
	for i, dep := range registeredProto.Dependencies {
		if dep != expectedDeps[i] {
			t.Errorf("expected dependency '%s', got '%s'", expectedDeps[i], dep)
		}
	}
}

// TestRegisterProto_MultipleMessages tests multiple message types
func TestRegisterProto_MultipleMessages(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("multi.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("Message1")},
				{Name: proto.String("Message2")},
				{Name: proto.String("Message3")},
			},
		},
	}

	resp, err := server.RegisterProto(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterProto failed: %v", err)
	}

	if len(resp.RegisteredMessages) != 3 {
		t.Errorf("expected 3 registered messages, got %d", len(resp.RegisteredMessages))
	}

	expectedMessages := []string{"Message1", "Message2", "Message3"}
	for i, msg := range resp.RegisteredMessages {
		if msg != expectedMessages[i] {
			t.Errorf("expected message '%s', got '%s'", expectedMessages[i], msg)
		}
	}
}

// TestRegisterService_MultipleMethods tests multiple methods
func TestRegisterService_MultipleMethods(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := &collector.RegisterServiceRequest{
		Namespace: "test",
		ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
			Name: proto.String("MultiMethodService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: proto.String("Method1")},
				{Name: proto.String("Method2")},
				{Name: proto.String("Method3")},
			},
		},
	}

	resp, err := server.RegisterService(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	if len(resp.RegisteredMethods) != 3 {
		t.Errorf("expected 3 registered methods, got %d", len(resp.RegisteredMethods))
	}

	expectedMethods := []string{"Method1", "Method2", "Method3"}
	for i, method := range resp.RegisteredMethods {
		if method != expectedMethods[i] {
			t.Errorf("expected method '%s', got '%s'", expectedMethods[i], method)
		}
	}
}

// TestRegisterProto_ComplexTypes tests nested types, enums, maps, oneofs
func TestRegisterProto_ComplexTypes(t *testing.T) {
	server, registeredProtos, _ := setupTestServer(t)

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("complex.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: proto.String("ComplexMessage"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{
							Name:   proto.String("id"),
							Number: proto.Int32(1),
							Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
						},
						{
							Name:     proto.String("nested"),
							Number:   proto.Int32(2),
							Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
							TypeName: proto.String(".test.NestedMessage"),
						},
						{
							Name:   proto.String("tags"),
							Number: proto.Int32(3),
							Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							Label:  descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						},
						{
							Name:     proto.String("metadata"),
							Number:   proto.Int32(4),
							Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
							TypeName: proto.String(".test.ComplexMessage.MetadataEntry"),
						},
					},
					NestedType: []*descriptorpb.DescriptorProto{
						{
							Name: proto.String("MetadataEntry"),
							Options: &descriptorpb.MessageOptions{
								MapEntry: proto.Bool(true),
							},
							Field: []*descriptorpb.FieldDescriptorProto{
								{
									Name:   proto.String("key"),
									Number: proto.Int32(1),
									Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
								},
								{
									Name:   proto.String("value"),
									Number: proto.Int32(2),
									Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
								},
							},
						},
					},
					OneofDecl: []*descriptorpb.OneofDescriptorProto{
						{Name: proto.String("test_oneof")},
					},
				},
				{
					Name: proto.String("NestedMessage"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{
							Name:   proto.String("value"),
							Number: proto.Int32(1),
							Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						},
					},
				},
			},
			EnumType: []*descriptorpb.EnumDescriptorProto{
				{
					Name: proto.String("Status"),
					Value: []*descriptorpb.EnumValueDescriptorProto{
						{Name: proto.String("UNKNOWN"), Number: proto.Int32(0)},
						{Name: proto.String("ACTIVE"), Number: proto.Int32(1)},
						{Name: proto.String("INACTIVE"), Number: proto.Int32(2)},
					},
				},
			},
		},
	}

	resp, err := server.RegisterProto(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterProto failed: %v", err)
	}

	// Should register top-level messages only
	if len(resp.RegisteredMessages) != 2 {
		t.Errorf("expected 2 top-level messages, got %d", len(resp.RegisteredMessages))
	}

	// Verify the full descriptor is stored with all nested info
	record, err := registeredProtos.GetRecord(context.Background(), resp.ProtoId)
	if err != nil {
		t.Fatalf("GetRecord failed: %v", err)
	}

	registeredProto := &collector.RegisteredProto{}
	err = proto.Unmarshal(record.ProtoData, registeredProto)
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Verify nested types are preserved in the descriptor
	if len(registeredProto.FileDescriptor.MessageType) != 2 {
		t.Errorf("expected 2 message types in descriptor, got %d", len(registeredProto.FileDescriptor.MessageType))
	}

	complexMsg := registeredProto.FileDescriptor.MessageType[0]
	if len(complexMsg.NestedType) != 1 {
		t.Errorf("expected 1 nested type, got %d", len(complexMsg.NestedType))
	}

	if len(complexMsg.OneofDecl) != 1 {
		t.Errorf("expected 1 oneof declaration, got %d", len(complexMsg.OneofDecl))
	}

	// Verify enum is preserved
	if len(registeredProto.FileDescriptor.EnumType) != 1 {
		t.Errorf("expected 1 enum type, got %d", len(registeredProto.FileDescriptor.EnumType))
	}
}

// TestRegisterService_StreamingMethods tests client/server/bidirectional streaming
func TestRegisterService_StreamingMethods(t *testing.T) {
	server, _, registeredServices := setupTestServer(t)

	req := &collector.RegisterServiceRequest{
		Namespace: "test",
		ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
			Name: proto.String("StreamingService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{
					Name:       proto.String("UnaryMethod"),
					InputType:  proto.String(".test.Request"),
					OutputType: proto.String(".test.Response"),
				},
				{
					Name:            proto.String("ServerStreamingMethod"),
					InputType:       proto.String(".test.Request"),
					OutputType:      proto.String(".test.Response"),
					ServerStreaming: proto.Bool(true),
				},
				{
					Name:            proto.String("ClientStreamingMethod"),
					InputType:       proto.String(".test.Request"),
					OutputType:      proto.String(".test.Response"),
					ClientStreaming: proto.Bool(true),
				},
				{
					Name:            proto.String("BidirectionalStreamingMethod"),
					InputType:       proto.String(".test.Request"),
					OutputType:      proto.String(".test.Response"),
					ClientStreaming: proto.Bool(true),
					ServerStreaming: proto.Bool(true),
				},
			},
		},
	}

	resp, err := server.RegisterService(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	// All methods should be registered
	if len(resp.RegisteredMethods) != 4 {
		t.Errorf("expected 4 methods, got %d", len(resp.RegisteredMethods))
	}

	// Verify the full descriptor preserves streaming information
	record, err := registeredServices.GetRecord(context.Background(), resp.ServiceId)
	if err != nil {
		t.Fatalf("GetRecord failed: %v", err)
	}

	registeredService := &collector.RegisteredService{}
	err = proto.Unmarshal(record.ProtoData, registeredService)
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	methods := registeredService.ServiceDescriptor.Method

	// Verify unary method
	if methods[0].GetServerStreaming() || methods[0].GetClientStreaming() {
		t.Error("unary method should not be streaming")
	}

	// Verify server streaming
	if !methods[1].GetServerStreaming() || methods[1].GetClientStreaming() {
		t.Error("server streaming method should have ServerStreaming=true only")
	}

	// Verify client streaming
	if methods[2].GetServerStreaming() || !methods[2].GetClientStreaming() {
		t.Error("client streaming method should have ClientStreaming=true only")
	}

	// Verify bidirectional streaming
	if !methods[3].GetServerStreaming() || !methods[3].GetClientStreaming() {
		t.Error("bidirectional method should have both streaming flags true")
	}
}

// TestRegisterProto_RecursiveTypes tests self-referencing message types
func TestRegisterProto_RecursiveTypes(t *testing.T) {
	server, registeredProtos, _ := setupTestServer(t)

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name: proto.String("recursive.proto"),
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: proto.String("TreeNode"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{
							Name:   proto.String("value"),
							Number: proto.Int32(1),
							Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						},
						{
							Name:     proto.String("children"),
							Number:   proto.Int32(2),
							Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
							TypeName: proto.String(".test.TreeNode"), // Self-reference
							Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						},
						{
							Name:     proto.String("parent"),
							Number:   proto.Int32(3),
							Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
							TypeName: proto.String(".test.TreeNode"), // Self-reference
						},
					},
				},
			},
		},
	}

	resp, err := server.RegisterProto(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterProto failed: %v", err)
	}

	// Verify recursive type is stored correctly
	record, err := registeredProtos.GetRecord(context.Background(), resp.ProtoId)
	if err != nil {
		t.Fatalf("GetRecord failed: %v", err)
	}

	registeredProto := &collector.RegisteredProto{}
	err = proto.Unmarshal(record.ProtoData, registeredProto)
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	treeNode := registeredProto.FileDescriptor.MessageType[0]
	if len(treeNode.Field) != 3 {
		t.Errorf("expected 3 fields, got %d", len(treeNode.Field))
	}

	// Verify self-references are preserved
	childrenField := treeNode.Field[1]
	if childrenField.GetTypeName() != ".test.TreeNode" {
		t.Errorf("expected children to reference .test.TreeNode, got %s", childrenField.GetTypeName())
	}

	parentField := treeNode.Field[2]
	if parentField.GetTypeName() != ".test.TreeNode" {
		t.Errorf("expected parent to reference .test.TreeNode, got %s", parentField.GetTypeName())
	}
}
