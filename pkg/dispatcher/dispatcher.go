package dispatcher

import (
	"context"
	"fmt"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
)

// DynamicDispatcher implements the CollectiveDispatcher service
// It handles dynamic dispatch of gRPC requests with basic Serve functionality
type DynamicDispatcher struct {
	pb.UnimplementedCollectiveDispatcherServer

	// serviceRegistry maps namespace+service to actual gRPC service implementations
	serviceRegistry map[string]interface{}

	// methodRegistry maps service+method names to their handlers
	methodRegistry map[string]grpc.MethodDesc
}

// New creates a new DynamicDispatcher instance
func New() *DynamicDispatcher {
	return &DynamicDispatcher{
		serviceRegistry: make(map[string]interface{}),
		methodRegistry:  make(map[string]grpc.MethodDesc),
	}
}

// Serve implements the basic Serve functionality that executes a gRPC request
// after receiving the serialized gRPC method from another Dispatcher
func (d *DynamicDispatcher) Serve(ctx context.Context, req *pb.ServeRequest) (*pb.ServeResponse, error) {
	if req == nil {
		return &pb.ServeResponse{
			Status: &pb.Status{
				Code:    pb.Status_INVALID_ARGUMENT,
				Message: "request cannot be nil",
			},
		}, nil
	}

	// Validate required fields
	if req.Namespace == "" {
		return &pb.ServeResponse{
			Status: &pb.Status{
				Code:    pb.Status_INVALID_ARGUMENT,
				Message: "namespace is required",
			},
		}, nil
	}

	if req.Service == nil {
		return &pb.ServeResponse{
			Status: &pb.Status{
				Code:    pb.Status_INVALID_ARGUMENT,
				Message: "service is required",
			},
		}, nil
	}

	if req.MethodName == "" {
		return &pb.ServeResponse{
			Status: &pb.Status{
				Code:    pb.Status_INVALID_ARGUMENT,
				Message: "method_name is required",
			},
		}, nil
	}

	// Create service key for lookup
	serviceKey := fmt.Sprintf("%s/%s/%s", req.Namespace, req.Service.Namespace, req.Service.ServiceName)

	// Look up the service implementation
	serviceImpl, exists := d.serviceRegistry[serviceKey]
	if !exists {
		return &pb.ServeResponse{
			Status: &pb.Status{
				Code:    pb.Status_NOT_FOUND,
				Message: fmt.Sprintf("service not found: %s", serviceKey),
			},
		}, nil
	}

	// Create method key for lookup
	methodKey := fmt.Sprintf("%s.%s", serviceKey, req.MethodName)

	// Look up the method descriptor
	methodDesc, exists := d.methodRegistry[methodKey]
	if !exists {
		return &pb.ServeResponse{
			Status: &pb.Status{
				Code:    pb.Status_NOT_FOUND,
				Message: fmt.Sprintf("method not found: %s", methodKey),
			},
		}, nil
	}

	// Execute the method using reflection-like approach
	// This is a basic implementation that would need to be enhanced
	// for full dynamic dispatch capabilities
	result, err := d.executeMethod(ctx, serviceImpl, methodDesc, req.Input)
	if err != nil {
		// Convert gRPC status errors to our Status format
		if st, ok := status.FromError(err); ok {
			return &pb.ServeResponse{
				Status: &pb.Status{
					Code:    convertGRPCCode(st.Code()),
					Message: st.Message(),
				},
			}, nil
		}

		return &pb.ServeResponse{
			Status: &pb.Status{
				Code:    pb.Status_INTERNAL,
				Message: fmt.Sprintf("execution failed: %v", err),
			},
		}, nil
	}

	return &pb.ServeResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: "success",
		},
		Output:     result,
		ExecutorId: "dynamic-dispatcher-basic",
	}, nil
}

// executeMethod performs the actual method execution
// This is a basic stub implementation that needs to be enhanced
func (d *DynamicDispatcher) executeMethod(ctx context.Context, serviceImpl interface{}, method grpc.MethodDesc, input *anypb.Any) (*anypb.Any, error) {
	// For now, this is a placeholder implementation
	// In a full implementation, this would:
	// 1. Deserialize the input based on the method's input type
	// 2. Call the actual method on the service implementation
	// 3. Serialize the result back to Any

	// Return a placeholder response for now
	response := &pb.Status{
		Code:    pb.Status_UNIMPLEMENTED,
		Message: "method execution not yet implemented",
	}

	return anypb.New(response)
}

// RegisterService registers a gRPC service implementation for dynamic dispatch
func (d *DynamicDispatcher) RegisterService(namespace, serviceName string, serviceImpl interface{}, methods []grpc.MethodDesc) {
	serviceKey := fmt.Sprintf("%s/%s", namespace, serviceName)
	d.serviceRegistry[serviceKey] = serviceImpl

	for _, method := range methods {
		methodKey := fmt.Sprintf("%s.%s", serviceKey, method.MethodName)
		d.methodRegistry[methodKey] = method
	}
}

// convertGRPCCode converts gRPC status codes to our Status codes
func convertGRPCCode(code codes.Code) pb.Status_Code {
	switch code {
	case codes.OK:
		return pb.Status_OK
	case codes.Canceled:
		return pb.Status_CANCELLED
	case codes.Unknown:
		return pb.Status_UNKNOWN
	case codes.InvalidArgument:
		return pb.Status_INVALID_ARGUMENT
	case codes.NotFound:
		return pb.Status_NOT_FOUND
	case codes.AlreadyExists:
		return pb.Status_ALREADY_EXISTS
	case codes.PermissionDenied:
		return pb.Status_PERMISSION_DENIED
	case codes.ResourceExhausted:
		return pb.Status_RESOURCE_EXHAUSTED
	case codes.FailedPrecondition:
		return pb.Status_FAILED_PRECONDITION
	case codes.Aborted:
		return pb.Status_ABORTED
	case codes.OutOfRange:
		return pb.Status_OUT_OF_RANGE
	case codes.Unimplemented:
		return pb.Status_UNIMPLEMENTED
	case codes.Internal:
		return pb.Status_INTERNAL
	case codes.Unavailable:
		return pb.Status_UNAVAILABLE
	case codes.DataLoss:
		return pb.Status_DATA_LOSS
	default:
		return pb.Status_UNKNOWN
	}
}

// Connect implements basic connection establishment (placeholder)
func (d *DynamicDispatcher) Connect(ctx context.Context, req *pb.ConnectRequest) (*pb.ConnectResponse, error) {
	return &pb.ConnectResponse{
		Status: &pb.Status{
			Code:    pb.Status_UNIMPLEMENTED,
			Message: "Connect not yet implemented",
		},
	}, nil
}

// Dispatch implements basic dispatch functionality (placeholder)
func (d *DynamicDispatcher) Dispatch(ctx context.Context, req *pb.DispatchRequest) (*pb.DispatchResponse, error) {
	return &pb.DispatchResponse{
		Status: &pb.Status{
			Code:    pb.Status_UNIMPLEMENTED,
			Message: "Dispatch not yet implemented",
		},
	}, nil
}
