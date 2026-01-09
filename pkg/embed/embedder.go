package embed

import "context"

// Embedder defines how to turn text into vectors.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}
