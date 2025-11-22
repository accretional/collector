package registry

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ValidationInterceptor creates a gRPC unary interceptor that validates incoming RPCs
// against the registry. It checks if the service and method are registered under
// the specified namespace.
func (s *RegistryServer) ValidationInterceptor(namespace string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Parse service and method from FullMethod
		// Format: /package.ServiceName/MethodName
		parts := strings.Split(info.FullMethod, "/")
		if len(parts) != 3 {
			// Invalid format, skip validation
			return handler(ctx, req)
		}

		serviceFullName := parts[1] // e.g., "collector.CollectionService"
		methodName := parts[2]

		// Extract service name (last part after the dot)
		serviceParts := strings.Split(serviceFullName, ".")
		if len(serviceParts) == 0 {
			return handler(ctx, req)
		}
		serviceName := serviceParts[len(serviceParts)-1]

		// Validate the method
		if err := s.ValidateMethod(ctx, namespace, serviceName, methodName); err != nil {
			// If validation fails, return an error
			return nil, status.Errorf(codes.Unimplemented,
				"method %s on service %s is not registered in namespace %s: %v",
				methodName, serviceName, namespace, err)
		}

		// Validation passed, proceed with the handler
		return handler(ctx, req)
	}
}

// StreamValidationInterceptor creates a gRPC stream interceptor that validates incoming RPCs
// against the registry.
func (s *RegistryServer) StreamValidationInterceptor(namespace string) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Parse service and method from FullMethod
		parts := strings.Split(info.FullMethod, "/")
		if len(parts) != 3 {
			// Invalid format, skip validation
			return handler(srv, ss)
		}

		serviceFullName := parts[1]
		methodName := parts[2]

		// Extract service name (last part after the dot)
		serviceParts := strings.Split(serviceFullName, ".")
		if len(serviceParts) == 0 {
			return handler(srv, ss)
		}
		serviceName := serviceParts[len(serviceParts)-1]

		// Validate the method
		if err := s.ValidateMethod(ss.Context(), namespace, serviceName, methodName); err != nil {
			return status.Errorf(codes.Unimplemented,
				"method %s on service %s is not registered in namespace %s: %v",
				methodName, serviceName, namespace, err)
		}

		// Validation passed, proceed with the handler
		return handler(srv, ss)
	}
}
