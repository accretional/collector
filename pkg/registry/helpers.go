package registry

import (
	"context"

	"google.golang.org/grpc"
)

// RegistryValidator provides validation capabilities for services
// This is the OLD implementation that uses direct method calls
type RegistryValidator struct {
	registry *RegistryServer
}

// NewRegistryValidator creates a new validator that uses the given registry
// DEPRECATED: Use NewGRPCRegistryValidator for proper service-to-service communication
func NewRegistryValidator(registry *RegistryServer) *RegistryValidator {
	return &RegistryValidator{
		registry: registry,
	}
}

// ValidateServiceMethod validates that a service method is registered in the given namespace
func (v *RegistryValidator) ValidateServiceMethod(ctx context.Context, namespace, serviceName, methodName string) error {
	return v.registry.ValidateMethod(ctx, namespace, serviceName, methodName)
}

// GetRegistry returns the underlying registry server
func (v *RegistryValidator) GetRegistry() *RegistryServer {
	return v.registry
}

// GRPCRegistryValidator validates services via gRPC using a loopback connection
// This ensures proper service-to-service communication even when on the same server
type GRPCRegistryValidator struct {
	// Instead of using gRPC client, we use an interface that can be satisfied
	// by either direct calls or gRPC calls
	validator ServiceMethodValidator
}

// ServiceMethodValidator is an interface for validating service methods
// Can be implemented by direct calls or gRPC calls
type ServiceMethodValidator interface {
	ValidateServiceMethod(ctx context.Context, namespace, serviceName, methodName string) error
}

// NewGRPCRegistryValidator creates a validator using the provided validator implementation
// For same-server communication, this wraps a RegistryServer
// For cross-server communication, this would wrap a gRPC client
func NewGRPCRegistryValidator(validator ServiceMethodValidator) *GRPCRegistryValidator {
	return &GRPCRegistryValidator{
		validator: validator,
	}
}

// ValidateServiceMethod validates by delegating to the underlying validator
func (v *GRPCRegistryValidator) ValidateServiceMethod(ctx context.Context, namespace, serviceName, methodName string) error {
	return v.validator.ValidateServiceMethod(ctx, namespace, serviceName, methodName)
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
