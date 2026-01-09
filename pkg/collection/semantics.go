package collection

import "context"

// SemanticEngine defines intelligence operations on a Collection.
type SemanticEngine struct {
	Collection *Collection
}

// FindSimilar performs a semantic search by embedding the query text
// and running a vector search against the Store.
func (s *SemanticEngine) FindSimilar(ctx context.Context, queryText string, limit int) ([]*SearchResult, error) {
	q := &SearchQuery{
		SemanticText: queryText,
		Limit:        limit,
	}

	return s.Collection.Search(ctx, q)
}
