package registry

import (
	"context"

	"google.golang.org/grpc"
)

// RegistryValidator provides validation capabilities for services
type RegistryValidator struct {
	registry *RegistryServer
}

// NewRegistryValidator creates a new validator that uses the given registry
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
