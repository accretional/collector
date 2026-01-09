package collection

import (
	"fmt"
	"sync"
)

// EmbedderFactory creates an Embedder for the given dimensions.
type EmbedderFactory func(dimensions int) Embedder

// embedderRegistry holds registered embedder factories.
var (
	embedderRegistry   = make(map[string]EmbedderFactory)
	embedderRegistryMu sync.RWMutex
)

func init() {
	// Register built-in embedders
	RegisterEmbedder("deterministic", func(dims int) Embedder {
		return NewDeterministicEmbedder(dims, 1)
	})
}

// RegisterEmbedder registers an embedder factory with the given name.
// This should be called during init() for built-in embedders or
// at startup for plugin embedders.
func RegisterEmbedder(name string, factory EmbedderFactory) {
	embedderRegistryMu.Lock()
	defer embedderRegistryMu.Unlock()
	embedderRegistry[name] = factory
}

// GetEmbedder retrieves an embedder by type name and dimensions.
// Returns an error if the embedder type is not registered.
func GetEmbedder(embedderType string, dimensions int) (Embedder, error) {
	if embedderType == "" {
		return nil, fmt.Errorf("embedder type is required")
	}

	embedderRegistryMu.RLock()
	factory, ok := embedderRegistry[embedderType]
	embedderRegistryMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown embedder type: %q", embedderType)
	}

	return factory(dimensions), nil
}

// ListEmbedders returns a list of registered embedder type names.
func ListEmbedders() []string {
	embedderRegistryMu.RLock()
	defer embedderRegistryMu.RUnlock()

	names := make([]string, 0, len(embedderRegistry))
	for name := range embedderRegistry {
		names = append(names, name)
	}
	return names
}
