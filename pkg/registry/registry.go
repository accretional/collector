package registry

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TypeRegistrar is an interface for registering message types.
// This allows optional integration with the type registry without circular dependencies.
type TypeRegistrar interface {
	RegisterFileDescriptor(ctx context.Context, namespace string, fileDesc *descriptorpb.FileDescriptorProto) error
}

type RegistryServer struct {
	collector.UnimplementedCollectorRegistryServer
	registeredProtos   *collection.Collection
	registeredServices *collection.Collection
	typeRegistrar      TypeRegistrar    // Optional: registers types when protos are registered
	sizeLimits         *ProtoSizeLimits // Configurable size limits for proto registration
}

func NewRegistryServer(registeredProtos, registeredServices *collection.Collection) *RegistryServer {
	return &RegistryServer{
		registeredProtos:   registeredProtos,
		registeredServices: registeredServices,
		sizeLimits:         DefaultProtoSizeLimits(),
	}
}

// SetSizeLimits sets custom size limits for proto registration.
// This allows configuring limits different from the defaults.
func (s *RegistryServer) SetSizeLimits(limits *ProtoSizeLimits) {
	s.sizeLimits = limits
}

// SetTypeRegistrar sets the type registrar for this server.
// This is optional - if not set, types are not registered in the type registry.
func (s *RegistryServer) SetTypeRegistrar(registrar TypeRegistrar) {
	s.typeRegistrar = registrar
}

// isWellKnownType checks if a proto file is a well-known type that doesn't require registration
func isWellKnownType(fileName string) bool {
	// Well-known types that don't require registration:
	// - Google protobuf standard types (google/protobuf/*)
	// - Google API types (google/api/*)
	// - Collector core types (collection.proto, registry.proto, etc.)
	wellKnownPrefixes := []string{
		"google/protobuf/",
		"google/api/",
	}

	// Collector core proto files (in root proto/ directory)
	collectorCoreTypes := []string{
		"collection.proto",
		"collection_repo.proto",
		"collection_server.proto",
		"console.proto",
		"dispatcher.proto",
		"registry.proto",
		"system.proto",
		"worker.proto",
	}

	// Check prefixes
	for _, prefix := range wellKnownPrefixes {
		if strings.HasPrefix(fileName, prefix) {
			return true
		}
	}

	// Check collector core types
	for _, coreType := range collectorCoreTypes {
		if fileName == coreType {
			return true
		}
	}

	return false
}

// getNamespaceHierarchy returns a namespace and all its parent namespaces
// For example, "team/project/service" returns ["team/project/service", "team/project", "team", ""]
func getNamespaceHierarchy(namespace string) []string {
	if namespace == "" {
		return []string{""}
	}

	var hierarchy []string
	current := namespace
	hierarchy = append(hierarchy, current)

	// Walk up the hierarchy by removing segments after the last "/"
	for strings.Contains(current, "/") {
		lastSlash := strings.LastIndex(current, "/")
		current = current[:lastSlash]
		hierarchy = append(hierarchy, current)
	}

	// Add root namespace
	hierarchy = append(hierarchy, "")

	return hierarchy
}

// validateDependencies checks that all dependencies exist and there are no circular dependencies
// Dependencies are resolved by searching the current namespace and walking up the namespace hierarchy
func (s *RegistryServer) validateDependencies(ctx context.Context, namespace string, fileName string, dependencies []string) error {
	if len(dependencies) == 0 {
		return nil
	}

	var missingDeps []string

	for _, dep := range dependencies {
		// Skip well-known types
		if isWellKnownType(dep) {
			continue
		}

		// Check for self-reference (circular dependency)
		if dep == fileName {
			return status.Errorf(codes.InvalidArgument, "circular dependency detected: proto %s depends on itself", fileName)
		}

		// Try to find dependency in current namespace and parent namespaces
		found := false
		namespaceHierarchy := getNamespaceHierarchy(namespace)

		for _, ns := range namespaceHierarchy {
			_, err := s.LookupProto(ctx, ns, dep)
			if err == nil {
				found = true
				break
			}
			// Continue to parent namespace if not found
			if status.Code(err) != codes.NotFound {
				return err // Return other errors immediately
			}
		}

		if !found {
			missingDeps = append(missingDeps, dep)
		}
	}

	if len(missingDeps) > 0 {
		return status.Errorf(codes.InvalidArgument, "missing dependencies (searched namespace %s and parents): %s",
			namespace, strings.Join(missingDeps, ", "))
	}

	return nil
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

	// Validate namespace
	if err := collection.ValidateNamespace(req.Namespace); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid namespace: %v", err)
	}

	// Validate proto name
	if err := collection.ValidateProtoFileName(req.FileDescriptor.GetName(), "proto name"); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid proto name: %v", err)
	}

	// Validate size limits (marshal proto to check size)
	marshaledProto, err := proto.Marshal(req.FileDescriptor)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal proto: %v", err)
	}
	if err := s.sizeLimits.ValidateProtoSizeLimits(req.FileDescriptor, len(marshaledProto)); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "proto size limit validation failed: %v", err)
	}

	registeredMessages := []string{}
	for _, msg := range req.FileDescriptor.MessageType {
		registeredMessages = append(registeredMessages, msg.GetName())
	}

	protoID := fmt.Sprintf("%s/%s", req.Namespace, req.FileDescriptor.GetName())

	// Check for duplicates
	_, err = s.registeredProtos.GetRecord(ctx, protoID)
	if err == nil {
		return nil, status.Errorf(codes.AlreadyExists, "proto already exists")
	} else if err != sql.ErrNoRows {
		// If it's not a "not found" error, return the error
		return nil, err
	}

	// Validate dependencies exist
	if err := s.validateDependencies(ctx, req.Namespace, req.FileDescriptor.GetName(), req.FileDescriptor.Dependency); err != nil {
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
		Metadata: &collector.Metadata{
			Labels: map[string]string{
				"namespace": req.Namespace,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// Register types in type registry if configured
	if s.typeRegistrar != nil {
		if err := s.typeRegistrar.RegisterFileDescriptor(ctx, req.Namespace, req.FileDescriptor); err != nil {
			// Log error but don't fail the registration - type registry is optional
			// In production, you might want to return this error or handle it differently
			_ = err
		}
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

	// Validate namespace
	if err := collection.ValidateNamespace(req.Namespace); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid namespace: %v", err)
	}

	// Validate service name
	if err := collection.ValidateServiceName(req.ServiceDescriptor.GetName(), "service name"); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid service name: %v", err)
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
		Metadata: &collector.Metadata{
			Labels: map[string]string{
				"namespace": req.Namespace,
			},
		},
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

// LookupProtoByMessageName searches for a registered proto that contains the given message name.
// It searches within the specified namespace and returns the first matching proto.
func (s *RegistryServer) LookupProtoByMessageName(ctx context.Context, namespace, messageName string) (*collector.RegisteredProto, error) {
	// Search for protos in this namespace that contain the message name
	// The messageNames field is stored as a JSON array in the jsontext column
	results, err := s.registeredProtos.Search(ctx, &collection.SearchQuery{
		LabelFilters: map[string]string{"namespace": namespace},
	})
	if err != nil {
		return nil, fmt.Errorf("search protos: %w", err)
	}

	// Check each result for the message name
	for _, result := range results {
		registeredProto := &collector.RegisteredProto{}
		if err := proto.Unmarshal(result.Record.ProtoData, registeredProto); err != nil {
			continue // Skip invalid records
		}
		for _, name := range registeredProto.MessageNames {
			if name == messageName {
				return registeredProto, nil
			}
		}
	}

	return nil, status.Errorf(codes.NotFound, "proto with message %s not found in namespace %s", messageName, namespace)
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
	// Use Search with LabelFilters for DB-level filtering (no limit)
	query := &collection.SearchQuery{
		LabelFilters: make(map[string]string),
		Limit:        0, // No limit - get all matching records
	}

	if namespace != "" {
		query.LabelFilters["namespace"] = namespace
	}

	searchResults, err := s.registeredProtos.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	var protos []*collector.RegisteredProto
	for _, result := range searchResults {
		registeredProto := &collector.RegisteredProto{}
		if err := proto.Unmarshal(result.Record.ProtoData, registeredProto); err != nil {
			return nil, err
		}
		protos = append(protos, registeredProto)
	}

	return protos, nil
}

// ListServices returns all registered services, optionally filtered by namespace
func (s *RegistryServer) ListServices(ctx context.Context, req *collector.ListServicesRequest) (*collector.ListServicesResponse, error) {
	// Use Search with LabelFilters for DB-level filtering (no limit)
	query := &collection.SearchQuery{
		LabelFilters: make(map[string]string),
		Limit:        0, // No limit - get all matching records
	}

	if req.Namespace != "" {
		query.LabelFilters["namespace"] = req.Namespace
	}

	searchResults, err := s.registeredServices.Search(ctx, query)
	if err != nil {
		return &collector.ListServicesResponse{
			Status: &collector.Status{
				Code:    collector.Status_INTERNAL,
				Message: err.Error(),
			},
		}, nil
	}

	var services []*collector.RegisteredService
	for _, result := range searchResults {
		registeredService := &collector.RegisteredService{}
		if err := proto.Unmarshal(result.Record.ProtoData, registeredService); err != nil {
			return &collector.ListServicesResponse{
				Status: &collector.Status{
					Code:    collector.Status_INTERNAL,
					Message: err.Error(),
				},
			}, nil
		}
		services = append(services, registeredService)
	}

	return &collector.ListServicesResponse{
		Status: &collector.Status{
			Code:    collector.Status_OK,
			Message: "Success",
		},
		Services: services,
	}, nil
}
