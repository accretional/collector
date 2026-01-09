package embed

import (
	"context"
	"fmt"

	pb "github.com/accretional/collector/gen/collector"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

func NewEmbedder(cfg *pb.SearchConfig) (Embedder, error) {
	if cfg == nil || !cfg.EnableVector {
		return nil, nil
	}

	if cfg.VectorDimensions <= 0 {
		return nil, fmt.Errorf("vector_dimensions must be > 0 when enable_vector is true")
	}

	embedderType := cfg.EmbedderType
	// Default to deterministic if unspecified
	if embedderType == pb.EmbedderType_EMBEDDER_UNSPECIFIED {
		embedderType = pb.EmbedderType_EMBEDDER_DETERMINISTIC
	}

	switch embedderType {
	case pb.EmbedderType_EMBEDDER_DETERMINISTIC:
		return newDeterministicEmbedder(cfg)
	default:
		return nil, fmt.Errorf("unsupported embedder type: %v", embedderType)
	}
}

// Uses a fixed seed (1) for deterministic behavior.
func newDeterministicEmbedder(cfg *pb.SearchConfig) (Embedder, error) {
	return NewDeterministicEmbedder(int(cfg.VectorDimensions), 1), nil
}
