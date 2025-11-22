package registry

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	if req.Namespace == "" {
		return nil, status.Errorf(codes.InvalidArgument, "namespace is required")
	}
	if req.FileDescriptor == nil {
		return nil, status.Errorf(codes.InvalidArgument, "file descriptor is required")
	}
	if req.FileDescriptor.GetName() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "file descriptor name is required")
	}

	registeredMessages := []string{}
	for _, msg := range req.FileDescriptor.MessageType {
		registeredMessages = append(registeredMessages, msg.GetName())
	}

	protoID := fmt.Sprintf("%s/%s", req.Namespace, req.FileDescriptor.GetName())

	// Check for duplicates
	_, err := s.registeredProtos.GetRecord(ctx, protoID)
	if err == nil {
		return nil, status.Errorf(codes.AlreadyExists, "proto already exists")
	} else if err != sql.ErrNoRows {
		// If it's not a "not found" error, return the error
		return nil, err
	}

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
	if req.Namespace == "" {
		return nil, status.Errorf(codes.InvalidArgument, "namespace is required")
	}
	if req.ServiceDescriptor == nil {
		return nil, status.Errorf(codes.InvalidArgument, "service descriptor is required")
	}
	if req.ServiceDescriptor.GetName() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "service descriptor name is required")
	}

	methodNames := []string{}
	for _, method := range req.ServiceDescriptor.Method {
		methodNames = append(methodNames, method.GetName())
	}

	serviceID := fmt.Sprintf("%s/%s", req.Namespace, req.ServiceDescriptor.GetName())

	// Check for duplicates
	_, err := s.registeredServices.GetRecord(ctx, serviceID)
	if err == nil {
		return nil, status.Errorf(codes.AlreadyExists, "service already exists")
	} else if err != sql.ErrNoRows {
		// If it's not a "not found" error, return the error
		return nil, err
	}

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
