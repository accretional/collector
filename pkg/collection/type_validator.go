package collection

import (
	"context"

	pb "github.com/accretional/collector/gen/collector"
)

// MessageTypeValidator validates that message types are registered before use.
// This interface allows dependency injection without circular dependencies.
type MessageTypeValidator interface {
	// ValidateCollectionMessageType validates that a collection's message type is registered
	ValidateCollectionMessageType(ctx context.Context, coll *pb.Collection) error
}
