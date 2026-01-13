package collection

import (
	"fmt"

	pb "github.com/accretional/collector/gen/collector"
)

const (
	DefaultVectorDimensions = 384
)

func NormalizeSearchConfig(cfg *pb.SearchConfig) *pb.SearchConfig {
	if cfg == nil {
		// Default to FTS and JSON enabled if no config is provided.
		return &pb.SearchConfig{
			EnableFts:        true,
			EnableJson:       true,
			EnableVector:     false,
			VectorDimensions: 0,
			EmbedderType:     pb.EmbedderType_EMBEDDER_DETERMINISTIC,
		}
	}

	normalized := &pb.SearchConfig{
		EnableFts:        cfg.EnableFts,
		EnableJson:       cfg.EnableJson,
		EnableVector:     cfg.EnableVector,
		VectorDimensions: cfg.VectorDimensions,
		EmbedderType:     cfg.EmbedderType,
	}

	// Default embedder type if unspecified
	if normalized.EmbedderType == pb.EmbedderType_EMBEDDER_UNSPECIFIED {
		normalized.EmbedderType = pb.EmbedderType_EMBEDDER_DETERMINISTIC
	}

	if normalized.EnableVector && normalized.VectorDimensions <= 0 {
		normalized.VectorDimensions = DefaultVectorDimensions
	}

	return normalized
}

func ValidateSearchConfig(cfg *pb.SearchConfig) error {
	if cfg == nil {
		return fmt.Errorf("search config cannot be nil")
	}

	if cfg.EnableVector {
		if cfg.VectorDimensions <= 0 {
			return fmt.Errorf("vector_dimensions must be > 0 when enable_vector is true, got %d", cfg.VectorDimensions)
		}
		if cfg.VectorDimensions > 10000 {
			return fmt.Errorf("vector_dimensions is unreasonably large: %d (max: 10000)", cfg.VectorDimensions)
		}
	}

	return nil
}

func ValidateCollectionConfig(cfg *pb.CollectionConfig) error {
	if cfg == nil {
		return nil
	}

	if cfg.SearchConfig != nil {
		normalized := NormalizeSearchConfig(cfg.SearchConfig)
		return ValidateSearchConfig(normalized)
	}

	return nil
}
