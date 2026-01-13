package embed

import (
	"context"
	"testing"
)

func TestDeterministicEmbedder_Embed(t *testing.T) {
	ctx := context.Background()
	embedder := NewDeterministicEmbedder(16, 1)

	vec, err := embedder.Embed(ctx, "hello world")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(vec) != 16 {
		t.Errorf("expected vector length 16, got %d", len(vec))
	}

	// Check that vector is not all zeros
	allZero := true
	for _, v := range vec {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("expected non-zero vector")
	}
}

func TestDeterministicEmbedder_Embed_EmptyText(t *testing.T) {
	ctx := context.Background()
	embedder := NewDeterministicEmbedder(8, 1)

	vec, err := embedder.Embed(ctx, "")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(vec) != 8 {
		t.Errorf("expected vector length 8, got %d", len(vec))
	}

	// Empty text should produce zero vector
	for i, v := range vec {
		if v != 0 {
			t.Errorf("expected zero vector for empty text, but vec[%d] = %f", i, v)
		}
	}
}

func TestDeterministicEmbedder_Embed_WhitespaceOnly(t *testing.T) {
	ctx := context.Background()
	embedder := NewDeterministicEmbedder(8, 1)

	vec, err := embedder.Embed(ctx, "   \t\n  ")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	// Whitespace-only text should produce zero vector
	for i, v := range vec {
		if v != 0 {
			t.Errorf("expected zero vector for whitespace-only text, but vec[%d] = %f", i, v)
		}
	}
}

func TestDeterministicEmbedder_Embed_Deterministic(t *testing.T) {
	ctx := context.Background()
	embedder := NewDeterministicEmbedder(32, 42)

	text := "machine learning artificial intelligence"
	vec1, err := embedder.Embed(ctx, text)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	vec2, err := embedder.Embed(ctx, text)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(vec1) != len(vec2) {
		t.Fatalf("vector lengths differ: %d vs %d", len(vec1), len(vec2))
	}

	for i := range vec1 {
		if vec1[i] != vec2[i] {
			t.Errorf("vectors differ at index %d: %f != %f", i, vec1[i], vec2[i])
		}
	}
}

func TestDeterministicEmbedder_Embed_DifferentSeeds(t *testing.T) {
	ctx := context.Background()
	embedder1 := NewDeterministicEmbedder(16, 1)
	embedder2 := NewDeterministicEmbedder(16, 2)

	text := "test text"
	vec1, err := embedder1.Embed(ctx, text)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	vec2, err := embedder2.Embed(ctx, text)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	// Different seeds should produce different vectors
	identical := true
	for i := range vec1 {
		if vec1[i] != vec2[i] {
			identical = false
			break
		}
	}
	if identical {
		t.Error("expected different vectors for different seeds")
	}
}

func TestDeterministicEmbedder_Embed_DifferentDimensions(t *testing.T) {
	ctx := context.Background()
	embedder8 := NewDeterministicEmbedder(8, 1)
	embedder16 := NewDeterministicEmbedder(16, 1)

	text := "test"
	vec8, err := embedder8.Embed(ctx, text)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	vec16, err := embedder16.Embed(ctx, text)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(vec8) != 8 {
		t.Errorf("expected length 8, got %d", len(vec8))
	}
	if len(vec16) != 16 {
		t.Errorf("expected length 16, got %d", len(vec16))
	}
}

func TestDeterministicEmbedder_Embed_InvalidDimension(t *testing.T) {
	ctx := context.Background()
	embedder := NewDeterministicEmbedder(0, 1)

	_, err := embedder.Embed(ctx, "test")
	if err == nil {
		t.Error("expected error for invalid dimension")
	}
}

func TestDeterministicEmbedder_Embed_NegativeDimension(t *testing.T) {
	ctx := context.Background()
	embedder := NewDeterministicEmbedder(-1, 1)

	_, err := embedder.Embed(ctx, "test")
	if err == nil {
		t.Error("expected error for negative dimension")
	}
}

func TestDeterministicEmbedder_Embed_MultipleTokens(t *testing.T) {
	ctx := context.Background()
	embedder := NewDeterministicEmbedder(64, 1)

	text := "one two three four five"
	vec, err := embedder.Embed(ctx, text)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(vec) != 64 {
		t.Errorf("expected length 64, got %d", len(vec))
	}

	// Multiple tokens should produce non-zero values
	nonZeroCount := 0
	for _, v := range vec {
		if v != 0 {
			nonZeroCount++
		}
	}
	if nonZeroCount == 0 {
		t.Error("expected non-zero values for multiple tokens")
	}
}
