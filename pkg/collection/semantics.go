package collection

import (
	"context"

	"github.com/accretional/collector/pkg/embed"
)

// SemanticEngine defines intelligence operations on a Collection.
type SemanticEngine struct {
	Collection *Collection
	Embedder   embed.Embedder
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
