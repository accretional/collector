package security

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// DefaultAuthInterceptor is a no-op interceptor that allows all requests.
func DefaultAuthInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// Default: Allow everything
	return handler(ctx, req)
}

// WAFRule defines a rule for the Web Application Firewall.
// This is an example/testing implementation to demonstrate gRPC interceptor patterns.
// Production systems should implement domain-specific security policies.
type WAFRule struct {
	BlockedSubstrings []string
}

// NewWAFInterceptor creates an interceptor that blocks requests containing specific substrings.
// This is a demonstration interceptor for testing/validating general interceptor logic.
// It shows how to implement custom security policies as gRPC interceptors.
func NewWAFInterceptor(rules WAFRule) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// inspect request content
		if msg, ok := req.(proto.Message); ok {
			if err := checkMessage(msg, rules); err != nil {
				return nil, status.Errorf(codes.PermissionDenied, "WAF rejection: %v", err)
			}
		}
		return handler(ctx, req)
	}
}

// checkMessage recursively checks string fields in a protobuf message
func checkMessage(msg proto.Message, rules WAFRule) error {
	return checkMessageReflect(msg.ProtoReflect(), rules, "")
}

// checkMessageReflect recursively checks all string fields including nested messages
func checkMessageReflect(m protoreflect.Message, rules WAFRule, path string) error {
	var err error

	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		fieldPath := fd.Name()
		if path != "" {
			fieldPath = protoreflect.Name(fmt.Sprintf("%s.%s", path, fd.Name()))
		}

		switch fd.Kind() {
		case protoreflect.StringKind:
			if fd.IsList() {
				// Handle repeated string fields
				list := v.List()
				for i := 0; i < list.Len(); i++ {
					if checkErr := checkStringValue(list.Get(i).String(), string(fieldPath), rules); checkErr != nil {
						err = checkErr
						return false
					}
				}
			} else {
				if checkErr := checkStringValue(v.String(), string(fieldPath), rules); checkErr != nil {
					err = checkErr
					return false
				}
			}

		case protoreflect.MessageKind:
			if fd.IsMap() {
				// Handle map fields
				v.Map().Range(func(k protoreflect.MapKey, mapVal protoreflect.Value) bool {
					// Check string keys
					if fd.MapKey().Kind() == protoreflect.StringKind {
						if checkErr := checkStringValue(k.String(), string(fieldPath)+"[key]", rules); checkErr != nil {
							err = checkErr
							return false
						}
					}
					// Check string values
					if fd.MapValue().Kind() == protoreflect.StringKind {
						if checkErr := checkStringValue(mapVal.String(), string(fieldPath)+"[value]", rules); checkErr != nil {
							err = checkErr
							return false
						}
					}
					// Check nested message values
					if fd.MapValue().Kind() == protoreflect.MessageKind {
						if checkErr := checkMessageReflect(mapVal.Message(), rules, string(fieldPath)); checkErr != nil {
							err = checkErr
							return false
						}
					}
					return true
				})
				if err != nil {
					return false
				}
			} else if fd.IsList() {
				// Handle repeated message fields
				list := v.List()
				for i := 0; i < list.Len(); i++ {
					if checkErr := checkMessageReflect(list.Get(i).Message(), rules, string(fieldPath)); checkErr != nil {
						err = checkErr
						return false
					}
				}
			} else {
				// Handle singular nested message
				if checkErr := checkMessageReflect(v.Message(), rules, string(fieldPath)); checkErr != nil {
					err = checkErr
					return false
				}
			}
		}

		return true
	})

	return err
}

// checkStringValue checks a string value against WAF rules
func checkStringValue(strVal, fieldPath string, rules WAFRule) error {
	for _, blocked := range rules.BlockedSubstrings {
		if strings.Contains(strVal, blocked) {
			return fmt.Errorf("field '%s' contains blocked content '%s'", fieldPath, blocked)
		}
	}
	return nil
}
