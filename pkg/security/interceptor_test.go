package security

import (
	"context"
	"testing"

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
