package collection

import (
	"context"
	"fmt"
)

// Transport defines how a Collection is moved between nodes.
type Transport interface {
	// Clone creates a consistent copy of the DB at destPath.
	Clone(ctx context.Context, c *Collection, destPath string) error
	
	// Pack/Unpack could be added here for streaming
}

// SqliteTransport implements high-performance DB movement.
type SqliteTransport struct{}

func (t *SqliteTransport) Clone(ctx context.Context, c *Collection, destPath string) error {
	// Use SQLite's VACUUM INTO for consistent snapshots without locking.
	// This works because we exposed ExecuteRaw in the Store interface.
	return c.Store.ExecuteRaw(fmt.Sprintf("VACUUM INTO '%s'", destPath))
}
