package collection

import (
	"fmt"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// NewStaticJSONConverter creates a converter for a known proto.Message type.
// Use this for system collections where the type is known at compile time.
func NewStaticJSONConverter(msg proto.Message) ProtoToJSONConverter {
	msgType := msg.ProtoReflect().Type()
	return func(protoData []byte) (string, error) {
		// Create a new instance of the message type
		newMsg := msgType.New().Interface()
		if err := proto.Unmarshal(protoData, newMsg); err != nil {
			return "", fmt.Errorf("unmarshal proto: %w", err)
		}
		jsonBytes, err := protojson.Marshal(newMsg)
		if err != nil {
			return "", fmt.Errorf("marshal to json: %w", err)
		}
		return string(jsonBytes), nil
	}
}

// NewDynamicJSONConverter creates a converter that looks up the message type
// from a FileDescriptorProto and uses dynamic protobuf to convert.
// This is used for user-defined types where we don't have the Go type at compile time.
func NewDynamicJSONConverter(fileDesc *descriptorpb.FileDescriptorProto, messageName string) (ProtoToJSONConverter, error) {
	// Build a file descriptor from the proto
	fd, err := protodesc.NewFile(fileDesc, protoregistry.GlobalFiles)
	if err != nil {
		return nil, fmt.Errorf("create file descriptor: %w", err)
	}

	// Find the message descriptor
	var msgDesc protoreflect.MessageDescriptor
	for i := 0; i < fd.Messages().Len(); i++ {
		if fd.Messages().Get(i).Name() == protoreflect.Name(messageName) {
			msgDesc = fd.Messages().Get(i)
			break
		}
	}
	if msgDesc == nil {
		return nil, fmt.Errorf("message %q not found in file descriptor", messageName)
	}

	return func(protoData []byte) (string, error) {
		// Create a dynamic message
		msg := dynamicpb.NewMessage(msgDesc)
		if err := proto.Unmarshal(protoData, msg); err != nil {
			return "", fmt.Errorf("unmarshal proto: %w", err)
		}
		jsonBytes, err := protojson.Marshal(msg)
		if err != nil {
			return "", fmt.Errorf("marshal to json: %w", err)
		}
		return string(jsonBytes), nil
	}, nil
}

// SystemTypeConverters returns converters for well-known system types.
// These are used during bootstrap when the registry isn't available yet.
var SystemTypeConverters = map[string]ProtoToJSONConverter{
	"Collection":        NewStaticJSONConverter(&pb.Collection{}),
	"ValidationRule":    NewStaticJSONConverter(&pb.ValidationRule{}),
	"RegisteredProto":   NewStaticJSONConverter(&pb.RegisteredProto{}),
	"RegisteredService": NewStaticJSONConverter(&pb.RegisteredService{}),
	"Connection":        NewStaticJSONConverter(&pb.Connection{}),
	"AuditEvent":        NewStaticJSONConverter(&pb.AuditEvent{}),
	"SystemLog":         NewStaticJSONConverter(&pb.SystemLog{}),
}

// GetSystemTypeConverter returns a converter for a known system type by name.
// Returns nil if the type is not a known system type.
func GetSystemTypeConverter(messageName string) ProtoToJSONConverter {
	return SystemTypeConverters[messageName]
}

// ProtoLookupFunc looks up a FileDescriptorProto by namespace and message name.
// Returns the file descriptor and the message name, or an error if not found.
type ProtoLookupFunc func(namespace, messageName string) (*descriptorpb.FileDescriptorProto, error)

// DefaultJSONConverterFactory returns converters for system types only.
// For unknown types, returns nil which triggers the store's fallback behavior
// (use proto_data if valid JSON, otherwise "{}").
// Use NewRegistryConverterFactory for a factory that looks up types from the registry.
func DefaultJSONConverterFactory(namespace, messageName string) ProtoToJSONConverter {
	// System types are in the "collector" namespace
	if namespace == "collector" {
		if conv := SystemTypeConverters[messageName]; conv != nil {
			return conv
		}
	}
	// Unknown type - store will use fallback behavior
	return nil
}

// NewRegistryConverterFactory creates a converter factory that looks up types from the registry.
// It first checks system types, then uses the provided lookup function for dynamic types.
// The lookup function should return the FileDescriptorProto for a given namespace/message.
func NewRegistryConverterFactory(lookup ProtoLookupFunc) JSONConverterFactory {
	// Cache converters to avoid repeated lookups
	cache := make(map[string]ProtoToJSONConverter)

	return func(namespace, messageName string) ProtoToJSONConverter {
		// Check system types first (fast path for bootstrap)
		if namespace == "collector" {
			if conv := SystemTypeConverters[messageName]; conv != nil {
				return conv
			}
		}

		// Check cache
		key := namespace + "/" + messageName
		if conv, ok := cache[key]; ok {
			return conv
		}

		// Look up from registry
		fileDesc, err := lookup(namespace, messageName)
		if err != nil {
			// Type not found - return nil to trigger fallback
			return nil
		}

		// Create dynamic converter
		conv, err := NewDynamicJSONConverter(fileDesc, messageName)
		if err != nil {
			// Conversion setup failed - return nil to trigger fallback
			return nil
		}

		// Cache and return
		cache[key] = conv
		return conv
	}
}
