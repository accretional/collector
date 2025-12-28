package security

import (
	"context"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestWAFInterceptor(t *testing.T) {
	// Setup WAF rule
	rule := WAFRule{
		BlockedSubstrings: []string{"DROP TABLE", "alert(1)"},
	}
	interceptor := NewWAFInterceptor(rule)

	// Mock handler
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	tests := []struct {
		name      string
		input     proto.Message
		wantError bool
	}{
		{
			name: "Clean input",
			input: &descriptorpb.FileDescriptorProto{
				Name: proto.String("safe_file.proto"),
			},
			wantError: false,
		},
		{
			name: "Blocked input 1",
			input: &descriptorpb.FileDescriptorProto{
				Name: proto.String("file_with_DROP TABLE_injection.proto"),
			},
			wantError: true,
		},
		{
			name: "Blocked input 2",
			input: &descriptorpb.FileDescriptorProto{
				Name: proto.String("<script>alert(1)</script>"),
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := interceptor(context.Background(), tt.input, info, handler)
			if (err != nil) != tt.wantError {
				t.Errorf("interceptor() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestWAFInterceptorNestedMessages(t *testing.T) {
	rule := WAFRule{
		BlockedSubstrings: []string{"MALICIOUS"},
	}
	interceptor := NewWAFInterceptor(rule)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	tests := []struct {
		name      string
		input     proto.Message
		wantError bool
	}{
		{
			name: "Clean string field",
			input: &pb.ConnectRequest{
				Address: "localhost:8080",
			},
			wantError: false,
		},
		{
			name: "Blocked content in string field",
			input: &pb.ConnectRequest{
				Address: "MALICIOUS:8080",
			},
			wantError: true,
		},
		{
			name: "Repeated string field with clean values",
			input: &pb.ConnectRequest{
				Address:    "localhost:8080",
				Namespaces: []string{"production", "staging"},
			},
			wantError: false,
		},
		{
			name: "Repeated string field with blocked value",
			input: &pb.ConnectRequest{
				Address:    "localhost:8080",
				Namespaces: []string{"production", "MALICIOUS-ns"},
			},
			wantError: true,
		},
		{
			name: "Map with clean keys and values",
			input: &pb.ConnectRequest{
				Address: "localhost:8080",
				Metadata: map[string]string{
					"env":  "production",
					"team": "backend",
				},
			},
			wantError: false,
		},
		{
			name: "Map with blocked value",
			input: &pb.ConnectRequest{
				Address: "localhost:8080",
				Metadata: map[string]string{
					"comment": "This is MALICIOUS content",
				},
			},
			wantError: true,
		},
		{
			name: "Map with blocked key",
			input: &pb.ConnectRequest{
				Address: "localhost:8080",
				Metadata: map[string]string{
					"MALICIOUS_key": "safe_value",
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := interceptor(context.Background(), tt.input, info, handler)
			if (err != nil) != tt.wantError {
				t.Errorf("interceptor() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
