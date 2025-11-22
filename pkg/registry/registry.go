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

// LookupProto retrieves a registered proto by namespace and file name
func (s *RegistryServer) LookupProto(ctx context.Context, namespace, fileName string) (*collector.RegisteredProto, error) {
	protoID := fmt.Sprintf("%s/%s", namespace, fileName)
	record, err := s.registeredProtos.GetRecord(ctx, protoID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, status.Errorf(codes.NotFound, "proto %s not found", protoID)
		}
		return nil, err
	}

	registeredProto := &collector.RegisteredProto{}
	if err := proto.Unmarshal(record.ProtoData, registeredProto); err != nil {
		return nil, err
	}

	return registeredProto, nil
}

// LookupService retrieves a registered service by namespace and service name
func (s *RegistryServer) LookupService(ctx context.Context, req *collector.LookupServiceRequest) (*collector.LookupServiceResponse, error) {
	serviceID := fmt.Sprintf("%s/%s", req.Namespace, req.ServiceName)
	record, err := s.registeredServices.GetRecord(ctx, serviceID)
	if err != nil {
		if err == sql.ErrNoRows {
			return &collector.LookupServiceResponse{
				Status: &collector.Status{
					Code:    collector.Status_NOT_FOUND,
					Message: fmt.Sprintf("service %s not found", serviceID),
				},
			}, nil
		}
		return &collector.LookupServiceResponse{
			Status: &collector.Status{
				Code:    collector.Status_INTERNAL,
				Message: err.Error(),
			},
		}, nil
	}

	registeredService := &collector.RegisteredService{}
	if err := proto.Unmarshal(record.ProtoData, registeredService); err != nil {
		return &collector.LookupServiceResponse{
			Status: &collector.Status{
				Code:    collector.Status_INTERNAL,
				Message: err.Error(),
			},
		}, nil
	}

	return &collector.LookupServiceResponse{
		Status: &collector.Status{
			Code:    collector.Status_OK,
			Message: "Success",
		},
		Service: registeredService,
	}, nil
}

// ValidateService checks if a service is registered in the given namespace
func (s *RegistryServer) ValidateService(ctx context.Context, namespace, serviceName string) error {
	resp, err := s.LookupService(ctx, &collector.LookupServiceRequest{
		Namespace:   namespace,
		ServiceName: serviceName,
	})
	if err != nil {
		return err
	}
	if resp.Status.Code != collector.Status_OK {
		return status.Errorf(codes.NotFound, "%s", resp.Status.Message)
	}
	return nil
}

// ValidateMethod checks if a method exists on a registered service
func (s *RegistryServer) ValidateMethod(ctx context.Context, req *collector.ValidateMethodRequest) (*collector.ValidateMethodResponse, error) {
	resp, err := s.LookupService(ctx, &collector.LookupServiceRequest{
		Namespace:   req.Namespace,
		ServiceName: req.ServiceName,
	})
	if err != nil {
		return &collector.ValidateMethodResponse{
			Status: &collector.Status{
				Code:    collector.Status_INTERNAL,
				Message: err.Error(),
			},
			IsValid: false,
		}, nil
	}

	if resp.Status.Code != collector.Status_OK {
		return &collector.ValidateMethodResponse{
			Status:  resp.Status,
			IsValid: false,
		}, nil
	}

	service := resp.Service
	for _, method := range service.MethodNames {
		if method == req.MethodName {
			return &collector.ValidateMethodResponse{
				Status: &collector.Status{
					Code:    collector.Status_OK,
					Message: "Method is valid",
				},
				IsValid: true,
			}, nil
		}
	}

	return &collector.ValidateMethodResponse{
		Status: &collector.Status{
			Code:    collector.Status_NOT_FOUND,
			Message: fmt.Sprintf("method %s not found on service %s/%s", req.MethodName, req.Namespace, req.ServiceName),
		},
		IsValid: false,
	}, nil
}

// ListProtos returns all registered protos, optionally filtered by namespace
func (s *RegistryServer) ListProtos(ctx context.Context, namespace string) ([]*collector.RegisteredProto, error) {
	// TODO: Implement filtering when Collection supports prefix queries
	// For now, we'll get all records and filter manually
	records, err := s.registeredProtos.ListRecords(ctx, 0, 10000)
	if err != nil {
		return nil, err
	}

	var protos []*collector.RegisteredProto
	for _, record := range records {
		registeredProto := &collector.RegisteredProto{}
		if err := proto.Unmarshal(record.ProtoData, registeredProto); err != nil {
			return nil, err
		}

		if namespace == "" || registeredProto.Namespace == namespace {
			protos = append(protos, registeredProto)
		}
	}

	return protos, nil
}

// ListServices returns all registered services, optionally filtered by namespace
func (s *RegistryServer) ListServices(ctx context.Context, req *collector.ListServicesRequest) (*collector.ListServicesResponse, error) {
	// TODO: Implement filtering when Collection supports prefix queries
	// For now, we'll get all records and filter manually
	records, err := s.registeredServices.ListRecords(ctx, 0, 10000)
	if err != nil {
		return &collector.ListServicesResponse{
			Status: &collector.Status{
				Code:    collector.Status_INTERNAL,
				Message: err.Error(),
			},
		}, nil
	}

	var services []*collector.RegisteredService
	for _, record := range records {
		registeredService := &collector.RegisteredService{}
		if err := proto.Unmarshal(record.ProtoData, registeredService); err != nil {
			return &collector.ListServicesResponse{
				Status: &collector.Status{
					Code:    collector.Status_INTERNAL,
					Message: err.Error(),
				},
			}, nil
		}

		if req.Namespace == "" || registeredService.Namespace == req.Namespace {
			services = append(services, registeredService)
		}
	}

	return &collector.ListServicesResponse{
		Status: &collector.Status{
			Code:    collector.Status_OK,
			Message: "Success",
		},
		Services: services,
	}, nil
}
