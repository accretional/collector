package registry

import (
	"context"
	"fmt"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc"
)

// ServiceMethodValidator is an interface for validating service methods
// Can be implemented by gRPC clients or other validation mechanisms
type ServiceMethodValidator interface {
	ValidateServiceMethod(ctx context.Context, namespace, serviceName, methodName string) error
}

// GRPCRegistryValidator validates services via gRPC (recommended approach)
// This ensures proper service-to-service communication using the gRPC stack
type GRPCRegistryValidator struct {
	validator ServiceMethodValidator
}

// NewGRPCRegistryValidator creates a validator using the provided validator implementation
func NewGRPCRegistryValidator(validator ServiceMethodValidator) *GRPCRegistryValidator {
	return &GRPCRegistryValidator{
		validator: validator,
	}
}

// ValidateServiceMethod validates by delegating to the underlying validator
func (v *GRPCRegistryValidator) ValidateServiceMethod(ctx context.Context, namespace, serviceName, methodName string) error {
	return v.validator.ValidateServiceMethod(ctx, namespace, serviceName, methodName)
}

// RegistryServerValidator wraps a RegistryServer to provide validation
type RegistryServerValidator struct {
	server *RegistryServer
}

// NewRegistryValidator creates a validator from a RegistryServer
func NewRegistryValidator(server *RegistryServer) *RegistryServerValidator {
	return &RegistryServerValidator{server: server}
}

// ValidateServiceMethod implements the ServiceMethodValidator interface
func (v *RegistryServerValidator) ValidateServiceMethod(ctx context.Context, namespace, serviceName, methodName string) error {
	resp, err := v.server.ValidateMethod(ctx, &pb.ValidateMethodRequest{
		Namespace:   namespace,
		ServiceName: serviceName,
		MethodName:  methodName,
	})
	if err != nil {
		return err
	}
	if !resp.IsValid {
		return fmt.Errorf("method %s.%s not registered in namespace %s: %s", serviceName, methodName, namespace, resp.Message)
	}
	return nil
}

// WithValidation returns gRPC server options that add registry validation interceptors
// for the specified namespace.
func WithValidation(registry *RegistryServer, namespace string) []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.UnaryInterceptor(registry.ValidationInterceptor(namespace)),
		grpc.StreamInterceptor(registry.StreamValidationInterceptor(namespace)),
	}
}

// NewServerWithValidation creates a new gRPC server with registry validation enabled
func NewServerWithValidation(registry *RegistryServer, namespace string, opts ...grpc.ServerOption) *grpc.Server {
	validationOpts := WithValidation(registry, namespace)
	allOpts := append(validationOpts, opts...)
	return grpc.NewServer(allOpts...)
}
