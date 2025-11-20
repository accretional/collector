package collection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Fetcher handles retrieving Collection databases from remote sources.
type Fetcher struct {
	Client *http.Client
}

// FetchRemoteDB downloads a database and prepares a Collection.
// In a real scenario, this would inject a specific Store implementation factory.
func (f *Fetcher) FetchRemoteDB(ctx context.Context, url string, localPath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := f.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Write to temp file first
	tmp := localPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return err
	}
	out.Close()

	return os.Rename(tmp, localPath)
}
