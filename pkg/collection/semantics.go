package collection

import (
	"context"
)

// Embedder defines how to turn text into vectors.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// SemanticEngine defines intelligence operations on a Collection.
type SemanticEngine struct {
	Collection *Collection
	Embedder   Embedder
}

// FindSimilar performs a semantic search by embedding the query text 
// and running a vector search against the Store.
func (s *SemanticEngine) FindSimilar(ctx context.Context, queryText string, limit int) ([]*SearchResult, error) {
	vector, err := s.Embedder.Embed(ctx, queryText)
	if err != nil {
		return nil, err
	}

	q := &SearchQuery{
		Vector: vector,
		Limit:  limit,
	}
	
	return s.Collection.Search(ctx, q)
}
