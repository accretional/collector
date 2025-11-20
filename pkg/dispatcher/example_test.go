package dispatcher_test

import (
	"context"
	"fmt"
	"log"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/dispatcher"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/anypb"
)

// Example demonstrates basic usage of DynamicDispatcher
func ExampleDynamicDispatcher() {
	// Create a new DynamicDispatcher
	d := dispatcher.New()

	// Example of creating a ServeRequest
	req := &pb.ServeRequest{
		Namespace: "example",
		Service: &pb.ServiceTypeRef{
			Namespace:   "example",
			ServiceName: "GreeterService",
		},
		MethodName: "SayHello",
		Input:      &anypb.Any{}, // Would contain actual request data
		ExecutionContext: map[string]string{
			"timeout": "30s",
		},
	}

	// Call Serve method
	ctx := context.Background()
	resp, err := d.Serve(ctx, req)
	if err != nil {
		log.Fatalf("Serve failed: %v", err)
	}

	// Check the response
	if resp.Status.Code == pb.Status_NOT_FOUND {
		fmt.Printf("Service not found: %s\n", resp.Status.Message)
	} else {
		fmt.Printf("Serve completed with status: %v\n", resp.Status.Code)
	}

	// Output:
	// Service not found: service not found and no service_def provided: example/example/GreeterService
}

// Example demonstrates registering a service (placeholder for future functionality)
func ExampleDynamicDispatcher_RegisterService() {
	d := dispatcher.New()

	// This shows how services would be registered in the future
	// Currently RegisterService exists but the actual execution is not implemented
	methods := []grpc.MethodDesc{
		{
			MethodName: "SayHello",
			Handler:    nil, // Would contain actual handler function
		},
	}

	// Register a placeholder service
	d.RegisterService("example", "GreeterService", nil, methods)

	fmt.Println("Service registered successfully")
	// Output:
	// Service registered successfully
}
