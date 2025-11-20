package collection

import (
	"context"
	"fmt"
	"sync"
)

type InMemoryCollectionRepo struct {
	mu          sync.RWMutex
	collections map[string]*Collection
}

func NewInMemoryCollectionRepo() *InMemoryCollectionRepo {
	return &InMemoryCollectionRepo{
		collections: make(map[string]*Collection),
	}
}

func (r *InMemoryCollectionRepo) AddCollection(collection *Collection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%s/%s", collection.Meta.Namespace, collection.Meta.Name)
	r.collections[key] = collection
}

func (r *InMemoryCollectionRepo) GetCollection(ctx context.Context, namespace, name string) (*Collection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := fmt.Sprintf("%s/%s", namespace, name)
	collection, ok := r.collections[key]
	if !ok {
		return nil, fmt.Errorf("collection not found: %s/%s", namespace, name)
	}
	return collection, nil
}
