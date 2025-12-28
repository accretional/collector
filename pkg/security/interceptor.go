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

// WAFRule defines a rule for the Web Application Firewall
type WAFRule struct {
	BlockedSubstrings []string
}

// NewWAFInterceptor creates an interceptor that blocks requests containing specific substrings.
// This serves as an example of a custom security policy.
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
	m := msg.ProtoReflect()
	var err error

	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() == protoreflect.StringKind {
			strVal := v.String()
			for _, blocked := range rules.BlockedSubstrings {
				if strings.Contains(strVal, blocked) {
					err = fmt.Errorf("field '%s' contains blocked content '%s'", fd.Name(), blocked)
					return false // stop iteration
				}
			}
		}
		// TODO: handle nested messages if needed, but for simple WAF this is enough for now
		return true
	})

	return err
}
