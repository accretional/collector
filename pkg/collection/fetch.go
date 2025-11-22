package collection

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

// Fetcher handles retrieving Collections from remote sources.
type Fetcher struct {
	Client    *http.Client
	Transport Transport
}

// NewFetcher creates a new Fetcher with default settings.
func NewFetcher() *Fetcher {
	return &Fetcher{
		Client: &http.Client{
			Timeout: 5 * time.Minute,
		},
		Transport: &SqliteTransport{},
	}
}

// FetchRemoteDB downloads a collection database from a remote URL.
// The URL should point to a collection database file or packaged collection.
func (f *Fetcher) FetchRemoteDB(ctx context.Context, url string, localPath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := f.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Use transport to unpack the data
	if err := f.Transport.Unpack(ctx, resp.Body, localPath); err != nil {
		return fmt.Errorf("failed to unpack: %w", err)
	}

	return nil
}

// FetchFromStream reads collection data from an io.Reader and writes to localPath.
func (f *Fetcher) FetchFromStream(ctx context.Context, reader io.Reader, localPath string) error {
	return f.Transport.Unpack(ctx, reader, localPath)
}

// StreamToRemote uploads collection data to a remote endpoint.
func (f *Fetcher) StreamToRemote(ctx context.Context, collection *Collection, url string, includeFiles bool) error {
	// Pack the collection
	reader, size, err := f.Transport.Pack(ctx, collection, includeFiles)
	if err != nil {
		return fmt.Errorf("failed to pack collection: %w", err)
	}
	defer reader.Close()

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "PUT", url, reader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = size

	// Send request
	resp, err := f.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("upload failed with status: %s", resp.Status)
	}

	return nil
}

// CloneToLocal creates a local copy of a collection.
func (f *Fetcher) CloneToLocal(ctx context.Context, source *Collection, destPath string) error {
	return f.Transport.Clone(ctx, source, destPath)
}

// FetchWithProgress fetches a collection with progress reporting.
type ProgressReporter func(bytesRead int64, totalBytes int64)

func (f *Fetcher) FetchWithProgress(ctx context.Context, url string, localPath string, progress ProgressReporter) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := f.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	totalBytes := resp.ContentLength

	// Create progress reader
	progressReader := &progressReader{
		reader:   resp.Body,
		progress: progress,
		total:    totalBytes,
	}

	// Unpack with progress
	return f.Transport.Unpack(ctx, progressReader, localPath)
}

// progressReader wraps an io.Reader and reports progress.
type progressReader struct {
	reader   io.Reader
	progress ProgressReporter
	total    int64
	read     int64
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.read += int64(n)

	if pr.progress != nil {
		pr.progress(pr.read, pr.total)
	}

	return n, err
}

// ValidateRemoteDB performs basic validation on a fetched database.
func (f *Fetcher) ValidateRemoteDB(ctx context.Context, dbPath string) error {
	// Check file exists and is readable
	info, err := os.Stat(dbPath)
	if err != nil {
		return fmt.Errorf("database file not accessible: %w", err)
	}

	if info.Size() == 0 {
		return fmt.Errorf("database file is empty")
	}

	// Open database and run integrity check
	dsn := fmt.Sprintf("file:%s?mode=ro", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Run SQLite integrity check
	var result string
	err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result)
	if err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}

	if result != "ok" {
		return fmt.Errorf("database integrity check failed: %s", result)
	}

	return nil
}
