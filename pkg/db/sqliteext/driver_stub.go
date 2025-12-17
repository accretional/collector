//go:build !cgo

package sqliteext

import (
	"context"
	"database/sql"
	"fmt"
)

// DB is a stub for builds without CGo support.
type DB struct{}

func Open(ctx context.Context, dbPath, extensionPath, entryPoint string) (*DB, error) {
	return nil, fmt.Errorf("extension loading requires CGo build: use 'go build' with CGO_ENABLED=1")
}

func (d *DB) Close() error {
	return nil
}

func (d *DB) Conn() *sql.Conn {
	return nil
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return nil, fmt.Errorf("extension loading not available in this build")
}

func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return nil, fmt.Errorf("extension loading not available in this build")
}
