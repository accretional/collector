package db

import (
	"context"
	"fmt"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"github.com/accretional/collector/pkg/db/sqlite"
	"github.com/accretional/collector/pkg/embed"
)

type DBType string

const (
	DBTypeSQLite DBType = "sqlite"
	// DBTypePostgres DBType = "postgres" // Future
)

type Config struct {
	Type DBType

	// SQLite-specific
	SQLitePath string

	// Common
	SearchConfig *pb.SearchConfig
}

func NewStore(ctx context.Context, config Config, embedder embed.Embedder) (collection.Store, error) {
	if config.Type == "" {
		return nil, fmt.Errorf("database type is required (use db.DBTypeSQLite)")
	}

	switch config.Type {
	case DBTypeSQLite:
		return newSQLiteStore(ctx, config, embedder)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", config.Type)
	}
}

func newSQLiteStore(ctx context.Context, config Config, embedder embed.Embedder) (collection.Store, error) {
	if config.SQLitePath == "" {
		return nil, fmt.Errorf("SQLite path required for sqlite database type")
	}

	return sqlite.NewStore(config.SQLitePath, config.SearchConfig, embedder)
}
