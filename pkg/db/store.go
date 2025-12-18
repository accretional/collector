package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
)

const (
	CapabilityVector = "vector"
	CapabilityFTS    = "fts"
	CapabilityJSON   = "json"
)

type ExtensionConfig struct {
	Path       string
	EntryPoint string

	// If false, the store will be created but the extension
	// capability will be unavailable.
	Required bool
}

type StoreConfig struct {
	Type    string // "sqlite" (default)
	Path    string
	Options collection.Options

	Extensions []ExtensionConfig
}

func NewStore(ctx context.Context, config StoreConfig) (collection.Store, error) {
	storeType := config.Type
	if storeType == "" {
		storeType = "sqlite"
	}

	switch storeType {
	case "sqlite":
		return newSqliteStore(ctx, config)

	default:
		return nil, fmt.Errorf("unsupported store type: %s (supported: sqlite)", storeType)
	}
}

func newSqliteStore(ctx context.Context, config StoreConfig) (collection.Store, error) {
	extensions := make([]sqlite.ExtensionConfig, len(config.Extensions))
	for i, ext := range config.Extensions {
		extensions[i] = sqlite.ExtensionConfig{
			Path:       ext.Path,
			EntryPoint: ext.EntryPoint,
			Required:   ext.Required,
		}
	}

	store, err := sqlite.NewSqliteStore(ctx, config.Path, config.Options, extensions)
	if err != nil {
		if strings.Contains(err.Error(), "failed to load required extension") {
			for _, ext := range config.Extensions {
				if strings.Contains(err.Error(), ext.Path) {
					return nil, &ExtensionError{
						Extension: ext.Path,
						Cause:     err,
					}
				}
			}
		}
		return nil, fmt.Errorf("failed to create sqlite store: %w", err)
	}

	return store, nil
}
