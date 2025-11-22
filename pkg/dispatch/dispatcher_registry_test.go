package dispatch

import (
	"context"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/protobuf/types/known/anypb"
)

// MockValidator implements RegistryValidator for testing
type MockValidator struct {
	validServices map[string]map[string]bool // namespace -> service.method -> valid
	callCount     int
}

func NewMockValidator() *MockValidator {
	return &MockValidator{
		validServices: make(map[string]map[string]bool),
	}
}

func (m *MockValidator) RegisterService(namespace, serviceName, methodName string) {
	key := serviceName + "." + methodName
	if m.validServices[namespace] == nil {
		m.validServices[namespace] = make(map[string]bool)
	}
	m.validServices[namespace][key] = true
}

func (m *MockValidator) ValidateServiceMethod(ctx context.Context, namespace, serviceName, methodName string) error {
	m.callCount++
	key := serviceName + "." + methodName

	if methods, ok := m.validServices[namespace]; ok {
		if methods[key] {
			return nil
		}
	}

	return &ValidationError{
		Namespace:   namespace,
		ServiceName: serviceName,
		MethodName:  methodName,
	}
}

type ValidationError struct {
	Namespace   string
	ServiceName string
	MethodName  string
}

func (e *ValidationError) Error() string {
	return "service not registered"
}

func TestDispatcherWithRegistryValidation(t *testing.T) {
	ctx := context.Background()
	namespace := "test"

	// Create mock validator
	validator := NewMockValidator()

	// Register a test service
	validator.RegisterService(namespace, "TestService", "TestMethod")

	// Create dispatcher with validator
	dispatcher := NewDispatcherWithRegistry(
		"dispatcher-001",
		"localhost:50052",
		[]string{namespace},
		validator,
	)

	// Register handler locally
	dispatcher.RegisterService(namespace, "TestService", "TestMethod", func(ctx context.Context, input interface{}) (interface{}, error) {
		return &anypb.Any{TypeUrl: "test", Value: []byte("success")}, nil
	})

	// Test 1: Valid service call (registered in validator)
	t.Run("ValidServiceCall", func(t *testing.T) {
		resp, err := dispatcher.Serve(ctx, &pb.ServeRequest{
			Namespace: namespace,
			Service:   &pb.ServiceTypeRef{ServiceName: "TestService"},
			MethodName: "TestMethod",
			Input:      &anypb.Any{},
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if resp.Status.Code != 200 {
			t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
		}

		if validator.callCount != 1 {
			t.Errorf("expected validator to be called once, called %d times", validator.callCount)
		}
	})

	// Test 2: Invalid service call (not registered in validator)
	t.Run("InvalidServiceCall", func(t *testing.T) {
		validator.callCount = 0

		resp, err := dispatcher.Serve(ctx, &pb.ServeRequest{
			Namespace: namespace,
			Service:   &pb.ServiceTypeRef{ServiceName: "TestService"},
			MethodName: "UnregisteredMethod",
			Input:      &anypb.Any{},
		})

		if err != nil {
			t.Fatalf("expected no error (errors returned in response), got %v", err)
		}

		if resp.Status.Code == 200 {
			t.Errorf("expected non-200 status for unregistered method")
		}

		if validator.callCount != 1 {
			t.Errorf("expected validator to be called once, called %d times", validator.callCount)
		}
	})

	// Test 3: Wrong namespace
	t.Run("WrongNamespace", func(t *testing.T) {
		validator.callCount = 0

		resp, err := dispatcher.Serve(ctx, &pb.ServeRequest{
			Namespace: "wrong-namespace",
			Service:   &pb.ServiceTypeRef{ServiceName: "TestService"},
			MethodName: "TestMethod",
			Input:      &anypb.Any{},
		})

		if err != nil {
			t.Fatalf("expected no error (errors returned in response), got %v", err)
		}

		if resp.Status.Code == 200 {
			t.Errorf("expected non-200 status for wrong namespace")
		}

		if validator.callCount != 1 {
			t.Errorf("expected validator to be called once, called %d times", validator.callCount)
		}
	})
}

func TestDispatcherWithoutRegistryValidation(t *testing.T) {
	ctx := context.Background()
	namespace := "test"

	// Create dispatcher WITHOUT validator (backwards compatibility)
	dispatcher := NewDispatcher(
		"dispatcher-002",
		"localhost:50053",
		[]string{namespace},
	)

	// Register handler locally
	dispatcher.RegisterService(namespace, "TestService", "TestMethod", func(ctx context.Context, input interface{}) (interface{}, error) {
		return &anypb.Any{TypeUrl: "test", Value: []byte("success")}, nil
	})

	// Test: Should work without validation
	t.Run("NoValidation", func(t *testing.T) {
		resp, err := dispatcher.Serve(ctx, &pb.ServeRequest{
			Namespace: namespace,
			Service:   &pb.ServiceTypeRef{ServiceName: "TestService"},
			MethodName: "TestMethod",
			Input:      &anypb.Any{},
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if resp.Status.Code != 200 {
			t.Errorf("expected status 200, got %d: %s", resp.Status.Code, resp.Status.Message)
		}
	})
}

func TestSetRegistryValidator(t *testing.T) {
	ctx := context.Background()
	namespace := "test"

	// Create dispatcher without validator
	dispatcher := NewDispatcher(
		"dispatcher-003",
		"localhost:50054",
		[]string{namespace},
	)

	// Register handler
	dispatcher.RegisterService(namespace, "TestService", "TestMethod", func(ctx context.Context, input interface{}) (interface{}, error) {
		return &anypb.Any{TypeUrl: "test", Value: []byte("success")}, nil
	})

	// Test 1: Works without validator
	t.Run("BeforeValidator", func(t *testing.T) {
		resp, err := dispatcher.Serve(ctx, &pb.ServeRequest{
			Namespace: namespace,
			Service:   &pb.ServiceTypeRef{ServiceName: "TestService"},
			MethodName: "TestMethod",
			Input:      &anypb.Any{},
		})

		if err != nil || resp.Status.Code != 200 {
			t.Error("should work before validator is set")
		}
	})

	// Add validator later
	validator := NewMockValidator()
	validator.RegisterService(namespace, "TestService", "TestMethod")
	dispatcher.SetRegistryValidator(validator)

	// Test 2: Now uses validator
	t.Run("AfterValidator", func(t *testing.T) {
		resp, err := dispatcher.Serve(ctx, &pb.ServeRequest{
			Namespace: namespace,
			Service:   &pb.ServiceTypeRef{ServiceName: "TestService"},
			MethodName: "TestMethod",
			Input:      &anypb.Any{},
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if resp.Status.Code != 200 {
			t.Errorf("expected status 200 with valid service")
		}

		if validator.callCount != 1 {
			t.Errorf("validator should have been called")
		}
	})

	// Test 3: Validator rejects unregistered method
	t.Run("ValidatorRejects", func(t *testing.T) {
		validator.callCount = 0

		resp, err := dispatcher.Serve(ctx, &pb.ServeRequest{
			Namespace: namespace,
			Service:   &pb.ServiceTypeRef{ServiceName: "TestService"},
			MethodName: "UnregisteredMethod",
			Input:      &anypb.Any{},
		})

		if err != nil {
			t.Fatalf("expected no error (errors in response), got %v", err)
		}

		if resp.Status.Code == 200 {
			t.Error("validator should have rejected unregistered method")
		}

		if validator.callCount != 1 {
			t.Errorf("validator should have been called")
		}
	})
}
