package registry

import (
	"context"
	"fmt"

	"github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/protobuf/proto"
)

type RegistryServer struct {
	collector.UnimplementedCollectorRegistryServer
	registeredProtos   *collection.Collection
	registeredServices *collection.Collection
}

func NewRegistryServer(registeredProtos, registeredServices *collection.Collection) *RegistryServer {
	return &RegistryServer{
		registeredProtos:   registeredProtos,
		registeredServices: registeredServices,
	}
}

func (s *RegistryServer) RegisterProto(ctx context.Context, req *collector.RegisterProtoRequest) (*collector.RegisterProtoResponse, error) {
	if req.FileDescriptor == nil {
		return nil, fmt.Errorf("file descriptor is required")
	}

	registeredMessages := []string{}
	for _, msg := range req.FileDescriptor.MessageType {
		registeredMessages = append(registeredMessages, *msg.Name)
	}

	protoID := fmt.Sprintf("%s/%s", req.Namespace, req.FileDescriptor.GetName())

	registeredProto := &collector.RegisteredProto{
		Id:             protoID,
		Namespace:      req.Namespace,
		MessageNames:   registeredMessages,
		FileDescriptor: req.FileDescriptor,
		Dependencies:   req.FileDescriptor.Dependency,
	}

	data, err := proto.Marshal(registeredProto)
	if err != nil {
		return nil, err
	}

	err = s.registeredProtos.CreateRecord(ctx, &collector.CollectionRecord{
		Id:        protoID,
		ProtoData: data,
	})
	if err != nil {
		return nil, err
	}

	return &collector.RegisterProtoResponse{
		Status:             &collector.Status{Code: collector.Status_OK},
		ProtoId:            protoID,
		RegisteredMessages: registeredMessages,
	}, nil
}

func (s *RegistryServer) RegisterService(ctx context.Context, req *collector.RegisterServiceRequest) (*collector.RegisterServiceResponse, error) {
	methodNames := []string{}
	for _, method := range req.ServiceDescriptor.Method {
		methodNames = append(methodNames, *method.Name)
	}

	serviceID := fmt.Sprintf("%s/%s", req.Namespace, req.ServiceDescriptor.GetName())

	registeredService := &collector.RegisteredService{
		Id:                serviceID,
		Namespace:         req.Namespace,
		ServiceName:       req.ServiceDescriptor.GetName(),
		ServiceDescriptor: req.ServiceDescriptor,
		MethodNames:       methodNames,
	}

	data, err := proto.Marshal(registeredService)
	if err != nil {
		return nil, err
	}

	err = s.registeredServices.CreateRecord(ctx, &collector.CollectionRecord{
		Id:        serviceID,
		ProtoData: data,
	})
	if err != nil {
		return nil, err
	}

	return &collector.RegisterServiceResponse{
		Status:            &collector.Status{Code: collector.Status_OK},
		ServiceId:         serviceID,
		RegisteredMethods: methodNames,
	}, nil
}
