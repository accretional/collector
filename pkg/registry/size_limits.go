package registry

import (
	"fmt"

	"google.golang.org/protobuf/types/descriptorpb"
)

// ProtoSizeLimits defines configurable limits for proto registration to prevent DoS attacks
type ProtoSizeLimits struct {
	// MaxMessageTypes is the maximum number of message types allowed in a single proto file
	MaxMessageTypes int

	// MaxNestingDepth is the maximum nesting depth allowed for message types
	MaxNestingDepth int

	// MaxProtoSize is the maximum size in bytes of a marshaled FileDescriptorProto
	MaxProtoSize int64
}

// DefaultProtoSizeLimits returns default size limits for proto registration
func DefaultProtoSizeLimits() *ProtoSizeLimits {
	return &ProtoSizeLimits{
		MaxMessageTypes: 100000,
		MaxNestingDepth: 100,
		MaxProtoSize:    10 * 1024 * 1024, // 10MB
	}
}

// ValidateProtoSizeLimits validates that a FileDescriptorProto is within configured limits
func (limits *ProtoSizeLimits) ValidateProtoSizeLimits(fileDesc *descriptorpb.FileDescriptorProto, marshaledSize int) error {
	// Check total message type count
	totalMessages := countTotalMessageTypes(fileDesc)
	if totalMessages > limits.MaxMessageTypes {
		return fmt.Errorf("proto exceeds maximum message types limit: %d > %d", totalMessages, limits.MaxMessageTypes)
	}

	// Check nesting depth
	maxDepth := calculateMaxNestingDepth(fileDesc)
	if maxDepth > limits.MaxNestingDepth {
		return fmt.Errorf("proto exceeds maximum nesting depth: %d > %d", maxDepth, limits.MaxNestingDepth)
	}

	// Check marshaled size
	if int64(marshaledSize) > limits.MaxProtoSize {
		return fmt.Errorf("proto exceeds maximum size: %d bytes > %d bytes", marshaledSize, limits.MaxProtoSize)
	}

	return nil
}

// countTotalMessageTypes recursively counts all message types in a proto file
func countTotalMessageTypes(fileDesc *descriptorpb.FileDescriptorProto) int {
	count := 0
	for _, msg := range fileDesc.MessageType {
		count += countMessageTypesRecursive(msg)
	}
	return count
}

// countMessageTypesRecursive recursively counts message types including nested ones
func countMessageTypesRecursive(msg *descriptorpb.DescriptorProto) int {
	count := 1 // Count this message
	for _, nested := range msg.NestedType {
		count += countMessageTypesRecursive(nested)
	}
	return count
}

// calculateMaxNestingDepth calculates the maximum nesting depth in a proto file
func calculateMaxNestingDepth(fileDesc *descriptorpb.FileDescriptorProto) int {
	maxDepth := 0
	for _, msg := range fileDesc.MessageType {
		depth := calculateNestingDepthRecursive(msg, 1)
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	return maxDepth
}

// calculateNestingDepthRecursive recursively calculates nesting depth for a message
func calculateNestingDepthRecursive(msg *descriptorpb.DescriptorProto, currentDepth int) int {
	if len(msg.NestedType) == 0 {
		return currentDepth
	}

	maxDepth := currentDepth
	for _, nested := range msg.NestedType {
		depth := calculateNestingDepthRecursive(nested, currentDepth+1)
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	return maxDepth
}
