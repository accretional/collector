package embed

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
)

// DeterministicEmbedder is a lightweight, dependency-free embedder that produces stable,
// fixed-dimension vectors by hashing tokens. Intended for tests and local runs.
type DeterministicEmbedder struct {
	dim  int
	seed uint32
}

// NewDeterministicEmbedder creates a new DeterministicEmbedder with the given dimension and seed.
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
		hash := hasher.Sum32() ^ d.seed
		idx := int(hash % uint32(d.dim))
		sign := float32(1)
		if hash&0x80000000 != 0 {
			sign = -1
		}
		vec[idx] += sign
	}

	return vec, nil
}
