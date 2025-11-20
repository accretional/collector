package collection

import (
	"context"
	"net/http"
)

// Fetcher handles retrieving Collection databases from remote sources.
// This allows a client to download a full "Table" to query locally.
type Fetcher struct {
	Transport Transport
	Client    *http.Client
}

// FetchRemoteDB downloads a sqlite database from a URL and initializes a Collection around it.
// This enables "Serverless" read-replicas where the logic moves to the data.
func (f *Fetcher) FetchRemoteDB(ctx context.Context, url string, localPath string) (*Collection, error) {
	// 1. Stream download to temp location
	// 2. Verify integrity (checksums)
	// 3. Use Transport.Unpack or simple file move
	// 4. Initialize Read-Only Store
	return nil, nil
}

// HotSwap replaces the underlying DB of a live collection with a fresher fetched version.
func (f *Fetcher) HotSwap(ctx context.Context, target *Collection, newDBPath string) error {
	// SQLite specific logic:
	// 1. Close current Store connection.
	// 2. Swap file paths atomically.
	// 3. Re-open Store connection.
	return nil
}
