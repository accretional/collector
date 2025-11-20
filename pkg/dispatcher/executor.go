package dispatcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/amenzhinsky/go-memexec"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"
)

// ExecutionMode defines how the service binary should be executed
type ExecutionMode int

const (
	// ExecutionModeDirect executes binary directly in memory (unsafe but fast)
	ExecutionModeDirect ExecutionMode = iota
	// ExecutionModeContainer executes binary in a sandboxed container
	ExecutionModeContainer
)

// ExecutorConfig configures how method execution should be performed
type ExecutorConfig struct {
	Mode           ExecutionMode
	Timeout        time.Duration
	ContainerImage string
	MaxMemory      string
	MaxCPU         string
}

// DefaultExecutorConfig returns a default configuration for method execution
func DefaultExecutorConfig() *ExecutorConfig {
	return &ExecutorConfig{
		Mode:           ExecutionModeDirect, // Start with direct as suggested
		Timeout:        30 * time.Second,
		ContainerImage: "golang:1.21-alpine", // Go builder base image
		MaxMemory:      "512m",
		MaxCPU:         "1",
	}
}

// ServiceExecutor handles the execution of service binaries for gRPC method calls
type ServiceExecutor struct {
	config *ExecutorConfig
}

// NewServiceExecutor creates a new service executor with the given configuration
func NewServiceExecutor(config *ExecutorConfig) *ServiceExecutor {
	if config == nil {
		config = DefaultExecutorConfig()
	}
	return &ServiceExecutor{
		config: config,
	}
}

// ExecuteMethod executes a gRPC method using the provided service binary
func (e *ServiceExecutor) ExecuteMethod(ctx context.Context, req *pb.ServeRequest, serviceBinary []byte) (*anypb.Any, error) {
	switch e.config.Mode {
	case ExecutionModeDirect:
		return e.executeDirectly(ctx, req, serviceBinary)
	case ExecutionModeContainer:
		return e.executeInContainer(ctx, req, serviceBinary)
	default:
		return nil, fmt.Errorf("unsupported execution mode: %v", e.config.Mode)
	}
}

// executeDirectly executes the service binary directly in memory using go-memexec
func (e *ServiceExecutor) executeDirectly(ctx context.Context, req *pb.ServeRequest, serviceBinary []byte) (*anypb.Any, error) {
	if len(serviceBinary) == 0 {
		return nil, fmt.Errorf("service binary is empty")
	}

	// Create executable from binary data in memory
	exe, err := memexec.New(serviceBinary)
	if err != nil {
		return nil, fmt.Errorf("failed to create executable from binary: %w", err)
	}
	defer exe.Close()

	// Create a context with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	// Prepare arguments for the executable
	// The binary should expect: namespace, service_name, method_name, and input_data
	args := []string{
		req.Namespace,
		req.Service.ServiceName,
		req.MethodName,
	}

	// Serialize the input data
	var inputData []byte
	if req.Input != nil {
		data, marshalErr := protojson.Marshal(req.Input)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal input data: %w", marshalErr)
		}
		inputData = data
	} else {
		inputData = []byte("{}")
	}

	// Create and run the command
	cmd := exe.CommandContext(execCtx, args...)
	cmd.Stdin = nil      // No stdin for security
	cmd.Env = []string{} // Empty environment for security

	// Pass input data through environment variable (safer than stdin)
	cmd.Env = append(cmd.Env, fmt.Sprintf("GRPC_INPUT_DATA=%s", string(inputData)))

	// Execute the command and capture output
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	// The output should be a serialized protobuf message
	// For now, we'll wrap it in a generic response
	response := &pb.Status{
		Code:    pb.Status_OK,
		Message: string(output),
	}

	return anypb.New(response)
}

// executeInContainer executes the service binary in a sandboxed container
func (e *ServiceExecutor) executeInContainer(ctx context.Context, req *pb.ServeRequest, serviceBinary []byte) (*anypb.Any, error) {
	if len(serviceBinary) == 0 {
		return nil, fmt.Errorf("service binary is empty")
	}

	// Create a temporary file for the binary
	tempFile, err := os.CreateTemp("", "service-*.bin")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Write binary data to temp file
	if _, err := tempFile.Write(serviceBinary); err != nil {
		return nil, fmt.Errorf("failed to write binary to temp file: %w", err)
	}

	// Make the binary executable
	if err := os.Chmod(tempFile.Name(), 0755); err != nil {
		return nil, fmt.Errorf("failed to make binary executable: %w", err)
	}

	// Create a context with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	// Prepare Docker command
	// Mount the binary and execute it in a constrained container
	dockerArgs := []string{
		"run",
		"--rm",           // Remove container after execution
		"--network=none", // No network access for security
		"--memory=" + e.config.MaxMemory,
		"--cpus=" + e.config.MaxCPU,
		"--user=65534:65534", // Run as nobody user
		"--read-only",        // Read-only filesystem
		"--tmpfs=/tmp:exec,nodev,nosuid,size=100m", // Temporary filesystem for execution
		"-v", fmt.Sprintf("%s:/app/service:ro", tempFile.Name()), // Mount binary as read-only
		e.config.ContainerImage,
		"/app/service", // Execute the binary
		req.Namespace,
		req.Service.ServiceName,
		req.MethodName,
	}

	// Serialize the input data
	var inputData []byte
	if req.Input != nil {
		data, marshalErr := protojson.Marshal(req.Input)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal input data: %w", marshalErr)
		}
		inputData = data
	} else {
		inputData = []byte("{}")
	}

	// Create and run the Docker command
	cmd := exec.CommandContext(execCtx, "docker", dockerArgs...)
	cmd.Env = []string{fmt.Sprintf("GRPC_INPUT_DATA=%s", string(inputData))}

	// Execute the command and capture output
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("container execution failed: %w", err)
	}

	// The output should be a serialized protobuf message
	// For now, we'll wrap it in a generic response
	response := &pb.Status{
		Code:    pb.Status_OK,
		Message: string(output),
	}

	return anypb.New(response)
}
