package collection

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
)

// A lightweight, dependency-free embedder that produces stable,
// fixed-dimension vectors by hashing tokens. Intended for tests and local runs.
type DeterministicEmbedder struct {
	dim  int
	seed uint32
}

func NewDeterministicEmbedder(dim int, seed uint32) *DeterministicEmbedder {
	return &DeterministicEmbedder{dim: dim, seed: seed}
}

// Splits text on whitespace and hashes tokens into the vector space.
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
