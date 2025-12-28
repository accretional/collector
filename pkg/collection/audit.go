package collection

import (
	"context"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AuditLogger handles structured audit logging to the system/audit collection.
type AuditLogger struct {
	collection *Collection
}

// NewAuditLogger creates a new audit logger backed by the system/audit collection.
func NewAuditLogger(collection *Collection) *AuditLogger {
	return &AuditLogger{
		collection: collection,
	}
}

// Log records an audit event.
func (l *AuditLogger) Log(ctx context.Context, event *pb.AuditEvent) error {
	if l.collection == nil {
		return nil
	}

	// Ensure ID and timestamps
	if event.Id == "" {
		event.Id = uuid.New().String()
	}
	if event.Timestamp == nil {
		event.Timestamp = timestamppb.Now()
	}

	// Marshal event to proto
	protoData, err := proto.Marshal(event)
	if err != nil {
		return err
	}

	// Create record
	record := &pb.CollectionRecord{
		Id:        event.Id,
		ProtoData: protoData,
		Metadata: &pb.Metadata{
			CreatedAt: event.Timestamp,
			Labels: map[string]string{
				"operation": event.Operation,
				"namespace": event.Namespace,
				"actor_id":  event.ActorId,
			},
		},
	}

	// Persist to collection
	// We use CreateRecord directly. For high volume, we might want a buffered writer later.
	return l.collection.CreateRecord(ctx, record)
}

// UnaryServerInterceptor returns a gRPC interceptor that logs all unary RPCs.
func (l *AuditLogger) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		// Call handler
		resp, err := handler(ctx, req)

		duration := time.Since(start).Milliseconds()

		// Extract metadata
		var sourceIP string
		if p, ok := peer.FromContext(ctx); ok {
			sourceIP = p.Addr.String()
		}

		// Helper to safely marshal to Any
		toAny := func(msg interface{}) *anypb.Any {
			if pm, ok := msg.(proto.Message); ok {
				if anyMsg, err := anypb.New(pm); err == nil {
					return anyMsg
				}
			}
			return nil
		}

		// Determine operation and resource from method name
		// /collector.CollectionService/Create -> op=Create, resource=...
		// This is a simplification; ideally we'd parse the request to get namespace/collection
		// But request parsing depends on the specific message type.
		// For now, we log the method name as operation.

		status := &pb.Status{Code: 200, Message: "OK"}
		if err != nil {
			status.Code = 500 // Map gRPC code?
			status.Message = err.Error()
		}

		event := &pb.AuditEvent{
			Timestamp:    timestamppb.New(start),
			Operation:    info.FullMethod,
			SourceIp:     sourceIP,
			RequestData:  toAny(req),
			ResponseData: toAny(resp), // Be careful logging full responses, maybe limit size?
			DurationMs:   duration,
			Status:       status,
		}

		// Try to extract namespace/collection from request if possible
		// We can type assert for common request types
		if r, ok := req.(interface{ GetCollection() *pb.Collection }); ok {
			if c := r.GetCollection(); c != nil {
				event.Namespace = c.Namespace
				event.CollectionName = c.Name
			}
		} else if r, ok := req.(interface{ GetNamespace() string }); ok {
			event.Namespace = r.GetNamespace()
		}

		// Log in background to avoid blocking response?
		// For audit, we might WANT to block or at least ensure it's enqueued.
		// For now, simple synchronous log, ignoring errors to not break the flow.
		_ = l.Log(ctx, event)

		return resp, err
	}
}
