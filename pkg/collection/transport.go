package collection

import (
	"context"
	"io"
)

// Transport defines how a Collection (the DB file) is moved between nodes.
// Since the Store interface exposes Path(), we can copy the underlying artifact.
type Transport interface {
	// Pack creates a portable archive of the collection (DB + optionally Files).
	Pack(ctx context.Context, c *Collection, w io.Writer) error
	
	// Unpack restores a collection from an archive to a destination.
	Unpack(ctx context.Context, r io.Reader, destPath string) (*Collection, error)
	
	// Clone creates a consistent copy of the DB, possibly using SQLite's VACUUM INTO.
	Clone(ctx context.Context, c *Collection, destPath string) error
}

// SqliteTransport implements high-performance DB movement.
type SqliteTransport struct{}

func (t *SqliteTransport) Clone(ctx context.Context, c *Collection, destPath string) error {
	// Use SQLite's native backup capability or VACUUM INTO for consistent snapshots
	// without locking the database for writes (WAL mode allows this).
	store, ok := c.Store.(interface{ ExecuteRaw(string, ...interface{}) error })
	if !ok {
		return fmt.Errorf("store does not support raw execution for backup")
	}
	// "VACUUM INTO 'filename'" is the modern way to snapshot SQLite
	return store.ExecuteRaw(fmt.Sprintf("VACUUM INTO '%s'", destPath))
}
