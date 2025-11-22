package dispatch

import (
	"context"
	"fmt"
	"sync"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
)

// ServiceHandler is a function that handles a service method invocation
type ServiceHandler func(ctx context.Context, input interface{}) (interface{}, error)

// RegistryValidator is an interface for validating services against a registry
type RegistryValidator interface {
	ValidateServiceMethod(ctx context.Context, namespace, serviceName, methodName string) error
}

// Dispatcher implements the CollectiveDispatcher service
type Dispatcher struct {
	pb.UnimplementedCollectiveDispatcherServer

	connManager *ConnectionManager

	// Service registry for handling Serve requests
	services      map[string]map[string]ServiceHandler // namespace -> method -> handler
	servicesMutex sync.RWMutex

	// Optional registry validator for checking if services are registered
	registryValidator RegistryValidator
}

// NewDispatcher creates a new dispatcher instance
func NewDispatcher(collectorID, address string, namespaces []string) *Dispatcher {
	return &Dispatcher{
		connManager: NewConnectionManager(collectorID, address, namespaces),
		services:    make(map[string]map[string]ServiceHandler),
	}
}

// NewDispatcherWithRegistry creates a new dispatcher instance with registry validation
func NewDispatcherWithRegistry(collectorID, address string, namespaces []string, validator RegistryValidator) *Dispatcher {
	return &Dispatcher{
		connManager:       NewConnectionManager(collectorID, address, namespaces),
		services:          make(map[string]map[string]ServiceHandler),
		registryValidator: validator,
	}
}

// SetRegistryValidator sets the registry validator for this dispatcher
func (d *Dispatcher) SetRegistryValidator(validator RegistryValidator) {
	d.registryValidator = validator
}

// Connect handles incoming connection requests
func (d *Dispatcher) Connect(ctx context.Context, req *pb.ConnectRequest) (*pb.ConnectResponse, error) {
	return d.connManager.HandleConnect(ctx, req)
}

// Serve handles service method invocations from other collectors
func (d *Dispatcher) Serve(ctx context.Context, req *pb.ServeRequest) (*pb.ServeResponse, error) {
	// Validate request
	if req.Namespace == "" {
		return &pb.ServeResponse{
			Status: &pb.Status{Code: 400, Message: "namespace is required"},
		}, nil
	}

	if req.Service == nil || req.Service.ServiceName == "" {
		return &pb.ServeResponse{
			Status: &pb.Status{Code: 400, Message: "service is required"},
		}, nil
	}

	if req.MethodName == "" {
		return &pb.ServeResponse{
			Status: &pb.Status{Code: 400, Message: "method_name is required"},
		}, nil
	}

	// Validate against registry if validator is configured
	if d.registryValidator != nil {
		if err := d.registryValidator.ValidateServiceMethod(ctx, req.Namespace, req.Service.ServiceName, req.MethodName); err != nil {
			return &pb.ServeResponse{
				Status: &pb.Status{
					Code:    404,
					Message: fmt.Sprintf("service not registered in registry: %v", err),
				},
			}, nil
		}
	}

	// Look up the handler
	d.servicesMutex.RLock()
	namespaceMethods, ok := d.services[req.Namespace]
	if !ok {
		d.servicesMutex.RUnlock()
		return &pb.ServeResponse{
			Status: &pb.Status{
				Code:    404,
				Message: fmt.Sprintf("namespace '%s' not found", req.Namespace),
			},
		}, nil
	}

	methodKey := fmt.Sprintf("%s.%s", req.Service.ServiceName, req.MethodName)
	handler, ok := namespaceMethods[methodKey]
	d.servicesMutex.RUnlock()

	if !ok {
		return &pb.ServeResponse{
			Status: &pb.Status{
				Code:    404,
				Message: fmt.Sprintf("method '%s' not found in namespace '%s'", methodKey, req.Namespace),
			},
		}, nil
	}

	// Execute the handler
	output, err := handler(ctx, req.Input)
	if err != nil {
		return &pb.ServeResponse{
			Status: &pb.Status{
				Code:    500,
				Message: fmt.Sprintf("handler error: %v", err),
			},
		}, nil
	}

	return &pb.ServeResponse{
		Status: &pb.Status{
			Code:    200,
			Message: "OK",
		},
		Output:     output.(*anypb.Any),
		ExecutorId: d.connManager.collectorID,
	}, nil
}

// Dispatch routes a request to the appropriate collector
func (d *Dispatcher) Dispatch(ctx context.Context, req *pb.DispatchRequest) (*pb.DispatchResponse, error) {
	// Validate request
	if req.Namespace == "" {
		return &pb.DispatchResponse{
			Status: &pb.Status{Code: 400, Message: "namespace is required"},
		}, nil
	}

	if req.Service == nil || req.Service.ServiceName == "" {
		return &pb.DispatchResponse{
			Status: &pb.Status{Code: 400, Message: "service is required"},
		}, nil
	}

	if req.MethodName == "" {
		return &pb.DispatchResponse{
			Status: &pb.Status{Code: 400, Message: "method_name is required"},
		}, nil
	}

	// If target is specified, route directly
	if req.TargetCollectorId != "" {
		return d.dispatchToTarget(ctx, req)
	}

	// Otherwise, auto-route based on namespace
	return d.autoRoute(ctx, req)
}

// RegisterService registers a service handler for a namespace and method
func (d *Dispatcher) RegisterService(namespace, serviceName, methodName string, handler ServiceHandler) {
	d.servicesMutex.Lock()
	defer d.servicesMutex.Unlock()

	if d.services[namespace] == nil {
		d.services[namespace] = make(map[string]ServiceHandler)
	}

	methodKey := fmt.Sprintf("%s.%s", serviceName, methodName)
	d.services[namespace][methodKey] = handler
}

// ConnectTo initiates a connection to another collector
func (d *Dispatcher) ConnectTo(ctx context.Context, address string, namespaces []string) (*pb.ConnectResponse, error) {
	return d.connManager.ConnectTo(ctx, address, namespaces)
}

// GetConnectionManager returns the connection manager
func (d *Dispatcher) GetConnectionManager() *ConnectionManager {
	return d.connManager
}

// dispatchToTarget sends a request to a specific target collector
func (d *Dispatcher) dispatchToTarget(ctx context.Context, req *pb.DispatchRequest) (*pb.DispatchResponse, error) {
	// Find connection to target
	connections := d.connManager.ListConnections()
	var targetClient pb.CollectiveDispatcherClient
	var targetAddress string

	for _, conn := range connections {
		if conn.SourceCollectorId == req.TargetCollectorId || conn.TargetCollectorId == req.TargetCollectorId {
			targetAddress = conn.Address
			break
		}
	}

	if targetAddress == "" {
		return &pb.DispatchResponse{
			Status: &pb.Status{
				Code:    404,
				Message: fmt.Sprintf("no connection to collector '%s'", req.TargetCollectorId),
			},
		}, nil
	}

	// Get client for the target
	client, ok := d.connManager.GetClient(targetAddress)
	if !ok {
		return &pb.DispatchResponse{
			Status: &pb.Status{
				Code:    500,
				Message: fmt.Sprintf("client not found for address '%s'", targetAddress),
			},
		}, nil
	}

	targetClient = client

	// Send Serve request to target
	serveReq := &pb.ServeRequest{
		Namespace:  req.Namespace,
		Service:    req.Service,
		MethodName: req.MethodName,
		Input:      req.Input,
	}

	serveResp, err := targetClient.Serve(ctx, serveReq)
	if err != nil {
		return &pb.DispatchResponse{
			Status: &pb.Status{
				Code:    500,
				Message: fmt.Sprintf("serve RPC failed: %v", err),
			},
		}, nil
	}

	return &pb.DispatchResponse{
		Status:               serveResp.Status,
		Output:               serveResp.Output,
		HandledByCollectorId: serveResp.ExecutorId,
	}, nil
}

// autoRoute automatically routes a request based on namespace
func (d *Dispatcher) autoRoute(ctx context.Context, req *pb.DispatchRequest) (*pb.DispatchResponse, error) {
	// Try to handle locally first
	d.servicesMutex.RLock()
	namespaceMethods, hasNamespace := d.services[req.Namespace]
	if hasNamespace {
		methodKey := fmt.Sprintf("%s.%s", req.Service.ServiceName, req.MethodName)
		_, hasMethod := namespaceMethods[methodKey]
		if hasMethod {
			d.servicesMutex.RUnlock()
			// Handle locally
			serveResp, err := d.Serve(ctx, &pb.ServeRequest{
				Namespace:  req.Namespace,
				Service:    req.Service,
				MethodName: req.MethodName,
				Input:      req.Input,
			})
			if err != nil {
				return nil, err
			}
			return &pb.DispatchResponse{
				Status:               serveResp.Status,
				Output:               serveResp.Output,
				HandledByCollectorId: serveResp.ExecutorId,
			}, nil
		}
	}
	d.servicesMutex.RUnlock()

	// Find a connection that shares this namespace
	connections := d.connManager.ListConnections()
	for _, conn := range connections {
		for _, ns := range conn.SharedNamespaces {
			if ns == req.Namespace {
				// Try to dispatch to this collector
				client, ok := d.connManager.GetClient(conn.Address)
				if !ok {
					continue
				}

				serveReq := &pb.ServeRequest{
					Namespace:  req.Namespace,
					Service:    req.Service,
					MethodName: req.MethodName,
					Input:      req.Input,
				}

				serveResp, err := client.Serve(ctx, serveReq)
				if err != nil {
					continue
				}

				if serveResp.Status.Code == 200 {
					return &pb.DispatchResponse{
						Status:               serveResp.Status,
						Output:               serveResp.Output,
						HandledByCollectorId: serveResp.ExecutorId,
					}, nil
				}
			}
		}
	}

	return &pb.DispatchResponse{
		Status: &pb.Status{
			Code:    404,
			Message: fmt.Sprintf("no collector found for namespace '%s'", req.Namespace),
		},
	}, nil
}

// Shutdown closes all connections
func (d *Dispatcher) Shutdown() {
	d.connManager.CloseAll()
}

// Error wrapping helper
func wrapError(code codes.Code, msg string, err error) error {
	if err != nil {
		return status.Errorf(code, "%s: %v", msg, err)
	}
	return status.Error(code, msg)
}
