package dispatcher

import (
	"context"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestNew(t *testing.T) {
	d := New()
	if d == nil {
		t.Fatal("New() returned nil")
	}
	if d.serviceRegistry == nil {
		t.Error("serviceRegistry not initialized")
	}
	if d.methodRegistry == nil {
		t.Error("methodRegistry not initialized")
	}
}

func TestServe_NilRequest(t *testing.T) {
	d := New()
	ctx := context.Background()

	resp, err := d.Serve(ctx, nil)
	if err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	if resp.Status.Code != pb.Status_INVALID_ARGUMENT {
		t.Errorf("Expected INVALID_ARGUMENT, got %v", resp.Status.Code)
	}
}

func TestServe_MissingNamespace(t *testing.T) {
	d := New()
	ctx := context.Background()

	req := &pb.ServeRequest{
		// namespace missing
		Service: &pb.ServiceTypeRef{
			Namespace:   "test",
			ServiceName: "TestService",
		},
		MethodName: "TestMethod",
	}

	resp, err := d.Serve(ctx, req)
	if err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	if resp.Status.Code != pb.Status_INVALID_ARGUMENT {
		t.Errorf("Expected INVALID_ARGUMENT, got %v", resp.Status.Code)
	}
	if resp.Status.Message != "namespace is required" {
		t.Errorf("Expected 'namespace is required', got %s", resp.Status.Message)
	}
}

func TestServe_MissingService(t *testing.T) {
	d := New()
	ctx := context.Background()

	req := &pb.ServeRequest{
		Namespace: "test",
		// service missing
		MethodName: "TestMethod",
	}

	resp, err := d.Serve(ctx, req)
	if err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	if resp.Status.Code != pb.Status_INVALID_ARGUMENT {
		t.Errorf("Expected INVALID_ARGUMENT, got %v", resp.Status.Code)
	}
	if resp.Status.Message != "service is required" {
		t.Errorf("Expected 'service is required', got %s", resp.Status.Message)
	}
}

func TestServe_MissingMethodName(t *testing.T) {
	d := New()
	ctx := context.Background()

	req := &pb.ServeRequest{
		Namespace: "test",
		Service: &pb.ServiceTypeRef{
			Namespace:   "test",
			ServiceName: "TestService",
		},
		// method_name missing
	}

	resp, err := d.Serve(ctx, req)
	if err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	if resp.Status.Code != pb.Status_INVALID_ARGUMENT {
		t.Errorf("Expected INVALID_ARGUMENT, got %v", resp.Status.Code)
	}
	if resp.Status.Message != "method_name is required" {
		t.Errorf("Expected 'method_name is required', got %s", resp.Status.Message)
	}
}

func TestServe_ServiceNotFound(t *testing.T) {
	d := New()
	ctx := context.Background()

	req := &pb.ServeRequest{
		Namespace: "test",
		Service: &pb.ServiceTypeRef{
			Namespace:   "test",
			ServiceName: "TestService",
		},
		MethodName: "TestMethod",
		Input:      &anypb.Any{},
	}

	resp, err := d.Serve(ctx, req)
	if err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	if resp.Status.Code != pb.Status_NOT_FOUND {
		t.Errorf("Expected NOT_FOUND, got %v", resp.Status.Code)
	}
	if resp.Status.Message != "service not found: test/test/TestService" {
		t.Errorf("Unexpected error message: %s", resp.Status.Message)
	}
}

func TestConnect_Placeholder(t *testing.T) {
	d := New()
	ctx := context.Background()

	req := &pb.ConnectRequest{
		Address: "localhost:8080",
	}

	resp, err := d.Connect(ctx, req)
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	// Currently returns UNIMPLEMENTED as placeholder
	if resp.Status.Code != pb.Status_UNIMPLEMENTED {
		t.Errorf("Expected UNIMPLEMENTED, got %v", resp.Status.Code)
	}
}

func TestDispatch_Placeholder(t *testing.T) {
	d := New()
	ctx := context.Background()

	req := &pb.DispatchRequest{
		Namespace: "test",
		Service: &pb.ServiceTypeRef{
			Namespace:   "test",
			ServiceName: "TestService",
		},
		MethodName: "TestMethod",
	}

	resp, err := d.Dispatch(ctx, req)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	// Currently returns UNIMPLEMENTED as placeholder
	if resp.Status.Code != pb.Status_UNIMPLEMENTED {
		t.Errorf("Expected UNIMPLEMENTED, got %v", resp.Status.Code)
	}
}

func TestConvertGRPCCode(t *testing.T) {
	tests := []struct {
		name     string
		input    codes.Code
		expected pb.Status_Code
	}{
		{"OK", codes.OK, pb.Status_OK},
		{"Canceled", codes.Canceled, pb.Status_CANCELLED},
		{"Unknown", codes.Unknown, pb.Status_UNKNOWN},
		{"InvalidArgument", codes.InvalidArgument, pb.Status_INVALID_ARGUMENT},
		{"NotFound", codes.NotFound, pb.Status_NOT_FOUND},
		{"AlreadyExists", codes.AlreadyExists, pb.Status_ALREADY_EXISTS},
		{"PermissionDenied", codes.PermissionDenied, pb.Status_PERMISSION_DENIED},
		{"ResourceExhausted", codes.ResourceExhausted, pb.Status_RESOURCE_EXHAUSTED},
		{"FailedPrecondition", codes.FailedPrecondition, pb.Status_FAILED_PRECONDITION},
		{"Aborted", codes.Aborted, pb.Status_ABORTED},
		{"OutOfRange", codes.OutOfRange, pb.Status_OUT_OF_RANGE},
		{"Unimplemented", codes.Unimplemented, pb.Status_UNIMPLEMENTED},
		{"Internal", codes.Internal, pb.Status_INTERNAL},
		{"Unavailable", codes.Unavailable, pb.Status_UNAVAILABLE},
		{"DataLoss", codes.DataLoss, pb.Status_DATA_LOSS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertGRPCCode(tt.input)
			if result != tt.expected {
				t.Errorf("convertGRPCCode(%v) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}
