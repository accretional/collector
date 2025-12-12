package collection

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
)

// DeterministicEmbedder is a lightweight, dependency-free embedder that
// produces stable, fixed-dimension vectors by hashing tokens. It is intended
// for tests and local runs where a real model is not available.
type DeterministicEmbedder struct {
	dim  int
	seed uint32
}

// NewDeterministicEmbedder returns a deterministic embedder with the given
// dimensionality. Seed can be used to vary the projection while remaining
// deterministic across runs.
func NewDeterministicEmbedder(dim int, seed uint32) *DeterministicEmbedder {
	return &DeterministicEmbedder{dim: dim, seed: seed}
}

// Embed splits text on whitespace and hashes tokens into the vector space.
func (d *DeterministicEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if d.dim <= 0 {
		return nil, fmt.Errorf("invalid dimension: %d", d.dim)
	}

	vec := make([]float32, d.dim)
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return vec, nil
	}

	hasher := fnv.New32a()
	for _, token := range parts {
		hasher.Reset()
		_, _ = hasher.Write([]byte(token))
		// Mix in the seed for reproducibility across processes.
		hash := hasher.Sum32() ^ d.seed
		idx := int(hash % uint32(d.dim))
		// Use the high bit to decide sign so we get variation around 0.
		sign := float32(1)
		if hash&0x80000000 != 0 {
			sign = -1
		}
		vec[idx] += sign
	}

	return vec, nil
}
