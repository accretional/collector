package registry

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TestRegisterProto_TooManyMessageTypes tests rejection when proto has too many message types
func TestRegisterProto_TooManyMessageTypes(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Create a proto with more than MaxMessageTypes (100000)
	// For testing, we'll use a smaller limit if configurable, but test the concept
	messageTypes := make([]*descriptorpb.DescriptorProto, 100001)
	for i := 0; i < 100001; i++ {
		messageTypes[i] = &descriptorpb.DescriptorProto{
			Name: proto.String(fmt.Sprintf("Message%d", i)),
		}
	}

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name:        proto.String("large.proto"),
			MessageType: messageTypes,
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if err == nil {
		t.Error("Expected error when registering proto with too many message types, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Errorf("Expected gRPC status error, got %T", err)
	} else if st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error code, got %v", st.Code())
	}

	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "message") {
		t.Errorf("Error should mention message types limit, got: %v", err)
	}
}

// TestRegisterProto_DeeplyNestedMessages tests rejection of deeply nested messages
func TestRegisterProto_DeeplyNestedMessages(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Create deeply nested message structure (> 100 levels)
	// Build from innermost to outermost
	messages := make([]*descriptorpb.DescriptorProto, 101)
	for i := 0; i < 101; i++ {
		msg := &descriptorpb.DescriptorProto{
			Name: proto.String(fmt.Sprintf("Level%d", i)),
		}
		if i > 0 {
			// Nest previous message inside this one
			msg.NestedType = []*descriptorpb.DescriptorProto{messages[i-1]}
		}
		messages[i] = msg
	}

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name:        proto.String("deeply_nested.proto"),
			MessageType: []*descriptorpb.DescriptorProto{messages[100]}, // Root message with 100+ nested levels
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if err == nil {
		t.Error("Expected error when registering proto with deeply nested messages, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Errorf("Expected gRPC status error, got %T", err)
	} else if st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error code, got %v", st.Code())
	}

	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "nest") {
		t.Errorf("Error should mention nesting depth, got: %v", err)
	}
}

// TestRegisterProto_ProtoSizeTooLarge tests rejection when proto size exceeds limit
func TestRegisterProto_ProtoSizeTooLarge(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Create a proto that when marshaled exceeds MaxProtoSize
	// Use a large number of messages with long names and many fields
	messageTypes := make([]*descriptorpb.DescriptorProto, 10000)
	for i := 0; i < 10000; i++ {
		// Create message with many fields to increase size
		fields := make([]*descriptorpb.FieldDescriptorProto, 100)
		for j := 0; j < 100; j++ {
			fields[j] = &descriptorpb.FieldDescriptorProto{
				Name:   proto.String(strings.Repeat("field", 10) + fmt.Sprintf("%d", j)),
				Number: proto.Int32(int32(j + 1)),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			}
		}
		messageTypes[i] = &descriptorpb.DescriptorProto{
			Name:  proto.String(strings.Repeat("Message", 10) + fmt.Sprintf("%d", i)),
			Field: fields,
		}
	}

	fileDesc := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("huge.proto"),
		MessageType: messageTypes,
	}

	// Check that this proto is actually large when marshaled
	data, _ := proto.Marshal(fileDesc)
	t.Logf("Proto size: %d bytes", len(data))

	req := &collector.RegisterProtoRequest{
		Namespace:      "test",
		FileDescriptor: fileDesc,
	}

	_, err := server.RegisterProto(context.Background(), req)
	if err == nil {
		t.Error("Expected error when registering proto that exceeds size limit, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Errorf("Expected gRPC status error, got %T", err)
	} else if st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error code, got %v", st.Code())
	}

	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "size") {
		t.Errorf("Error should mention size limit, got: %v", err)
	}
}

// TestRegisterProto_WithinLimits tests that protos within all limits are accepted
func TestRegisterProto_WithinLimits(t *testing.T) {
	server, _, _ := setupTestServer(t)

	// Create a reasonable proto well within limits
	messageTypes := make([]*descriptorpb.DescriptorProto, 10)
	for i := 0; i < 10; i++ {
		fields := make([]*descriptorpb.FieldDescriptorProto, 5)
		for j := 0; j < 5; j++ {
			fields[j] = &descriptorpb.FieldDescriptorProto{
				Name:   proto.String(fmt.Sprintf("field%d", j)),
				Number: proto.Int32(int32(j + 1)),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			}
		}
		messageTypes[i] = &descriptorpb.DescriptorProto{
			Name:  proto.String(fmt.Sprintf("Message%d", i)),
			Field: fields,
		}
	}

	req := &collector.RegisterProtoRequest{
		Namespace: "test",
		FileDescriptor: &descriptorpb.FileDescriptorProto{
			Name:        proto.String("reasonable.proto"),
			MessageType: messageTypes,
		},
	}

	_, err := server.RegisterProto(context.Background(), req)
	if err != nil {
		t.Errorf("Expected success for proto within limits, got error: %v", err)
	}
}
