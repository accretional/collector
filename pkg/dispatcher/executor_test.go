package dispatcher

import (
	"context"
	"testing"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestNewServiceExecutor(t *testing.T) {
	// Test with nil config (should use defaults)
	exec := NewServiceExecutor(nil)
	if exec == nil {
		t.Fatal("NewServiceExecutor returned nil")
	}
	if exec.config == nil {
		t.Fatal("config is nil")
	}
	if exec.config.Mode != ExecutionModeDirect {
		t.Errorf("Expected ExecutionModeDirect, got %v", exec.config.Mode)
	}

	// Test with custom config
	config := &ExecutorConfig{
		Mode:           ExecutionModeContainer,
		Timeout:        10 * time.Second,
		ContainerImage: "golang:1.20",
		MaxMemory:      "256m",
		MaxCPU:         "0.5",
	}
	exec = NewServiceExecutor(config)
	if exec.config.Mode != ExecutionModeContainer {
		t.Errorf("Expected ExecutionModeContainer, got %v", exec.config.Mode)
	}
	if exec.config.Timeout != 10*time.Second {
		t.Errorf("Expected 10s timeout, got %v", exec.config.Timeout)
	}
}

func TestDefaultExecutorConfig(t *testing.T) {
	config := DefaultExecutorConfig()
	if config == nil {
		t.Fatal("DefaultExecutorConfig returned nil")
	}
	if config.Mode != ExecutionModeDirect {
		t.Errorf("Expected ExecutionModeDirect, got %v", config.Mode)
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("Expected 30s timeout, got %v", config.Timeout)
	}
	if config.ContainerImage != "golang:1.21-alpine" {
		t.Errorf("Expected golang:1.21-alpine, got %s", config.ContainerImage)
	}
}

func TestExecuteMethod_EmptyBinary(t *testing.T) {
	exec := NewServiceExecutor(nil)
	ctx := context.Background()

	req := &pb.ServeRequest{
		Namespace: "test",
		Service: &pb.ServiceTypeRef{
			Namespace:   "test",
			ServiceName: "TestService",
		},
		MethodName: "TestMethod",
	}

	// Test with empty binary
	_, err := exec.ExecuteMethod(ctx, req, []byte{})
	if err == nil {
		t.Error("Expected error for empty binary")
	}
	if err.Error() != "service binary is empty" {
		t.Errorf("Expected 'service binary is empty', got %s", err.Error())
	}

	// Test with nil binary
	_, err = exec.ExecuteMethod(ctx, req, nil)
	if err == nil {
		t.Error("Expected error for nil binary")
	}
	if err.Error() != "service binary is empty" {
		t.Errorf("Expected 'service binary is empty', got %s", err.Error())
	}
}

func TestExecuteMethod_UnsupportedMode(t *testing.T) {
	config := &ExecutorConfig{
		Mode:    ExecutionMode(999), // Invalid mode
		Timeout: 30 * time.Second,
	}
	exec := NewServiceExecutor(config)
	ctx := context.Background()

	req := &pb.ServeRequest{
		Namespace: "test",
		Service: &pb.ServiceTypeRef{
			Namespace:   "test",
			ServiceName: "TestService",
		},
		MethodName: "TestMethod",
	}

	_, err := exec.ExecuteMethod(ctx, req, []byte("fake binary"))
	if err == nil {
		t.Error("Expected error for unsupported mode")
	}
	if !contains(err.Error(), "unsupported execution mode") {
		t.Errorf("Expected 'unsupported execution mode', got %s", err.Error())
	}
}

// Note: We can't easily test the actual execution without having valid binaries
// These tests focus on the structure and error handling
// Integration tests would require actual service binaries

func TestExecuteDirectly_InvalidBinary(t *testing.T) {
	exec := NewServiceExecutor(&ExecutorConfig{
		Mode:    ExecutionModeDirect,
		Timeout: 1 * time.Second, // Short timeout for test
	})
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

	// Test with invalid binary data (not a valid executable)
	invalidBinary := []byte("not a valid binary")
	_, err := exec.executeDirectly(ctx, req, invalidBinary)
	if err == nil {
		t.Error("Expected error for invalid binary")
	}
	// Error could be either from memexec or from execution
	if !contains(err.Error(), "failed to create executable from binary") && !contains(err.Error(), "execution failed") {
		t.Errorf("Expected 'failed to create executable from binary' or 'execution failed' error, got %s", err.Error())
	}
}

func TestExecuteInContainer_DockerNotAvailable(t *testing.T) {
	// Skip this test if we don't want to test Docker availability
	t.Skip("Skipping Docker test - requires Docker to be available")

	exec := NewServiceExecutor(&ExecutorConfig{
		Mode:           ExecutionModeContainer,
		Timeout:        1 * time.Second,
		ContainerImage: "golang:1.21-alpine",
		MaxMemory:      "512m",
		MaxCPU:         "1",
	})
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

	// Test with fake binary data
	fakeBinary := []byte("fake binary data")
	_, err := exec.executeInContainer(ctx, req, fakeBinary)
	// This will likely fail due to Docker not being available or binary being invalid
	// But we're testing the code path
	if err == nil {
		t.Error("Expected error for fake binary in container")
	}
}

// contains helper function is defined in dispatcher_test.go
