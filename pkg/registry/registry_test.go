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
