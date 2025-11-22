package collection

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Transport defines how a Collection is moved between collectors.
type Transport interface {
	// Clone creates a consistent copy of the collection at destPath
	Clone(ctx context.Context, c *Collection, destPath string) error

	// Pack prepares a collection for transport (returns a reader for the data)
	Pack(ctx context.Context, c *Collection, includeFiles bool) (io.ReadCloser, int64, error)

	// Unpack receives collection data and creates a new collection
	Unpack(ctx context.Context, reader io.Reader, destPath string) error
}

// SqliteTransport implements collection transport using SQLite operations.
type SqliteTransport struct{}

// Clone creates a consistent snapshot of the collection database.
// Uses SQLite's online backup API for hot backup with minimal locking.
// This allows concurrent reads and writes during the backup process.
func (t *SqliteTransport) Clone(ctx context.Context, c *Collection, destPath string) error {
	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Use the online backup API for hot backup
	// This works with WAL mode and allows concurrent access
	if err := c.Store.Backup(ctx, destPath); err != nil {
		return fmt.Errorf("failed to backup database: %w", err)
	}

	return nil
}

// CloneFallback uses VACUUM INTO as a fallback if Backup is not available.
// Note: This acquires locks and may block writes temporarily.
func (t *SqliteTransport) CloneFallback(ctx context.Context, c *Collection, destPath string) error {
	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Use VACUUM INTO for consistent snapshot
	// This creates a complete copy but acquires locks during the operation
	query := fmt.Sprintf("VACUUM INTO '%s'", destPath)
	if err := c.Store.ExecuteRaw(query); err != nil {
		return fmt.Errorf("failed to clone database: %w", err)
	}

	return nil
}

// Pack prepares a collection for network transport.
// Creates a tarball containing the database and optionally files.
func (t *SqliteTransport) Pack(ctx context.Context, c *Collection, includeFiles bool) (io.ReadCloser, int64, error) {
	// Create temporary directory for packing
	tmpDir, err := os.MkdirTemp("", "collection-pack-*")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Clone database to temp location
	dbPath := filepath.Join(tmpDir, "collection.db")
	if err := t.Clone(ctx, c, dbPath); err != nil {
		return nil, 0, fmt.Errorf("failed to clone database: %w", err)
	}

	// TODO: If includeFiles, copy filesystem data
	// This would involve walking the filesystem and adding to tarball

	// For now, just return the database file
	file, err := os.Open(dbPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open packed database: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, fmt.Errorf("failed to stat packed database: %w", err)
	}

	return file, stat.Size(), nil
}

// Unpack receives collection data and creates a new collection.
func (t *SqliteTransport) Unpack(ctx context.Context, reader io.Reader, destPath string) error {
	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Write to temp file first, then rename atomically
	tmpPath := destPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpPath) // Clean up on error

	if _, err := io.Copy(tmpFile, reader); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write data: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to rename to destination: %w", err)
	}

	return nil
}

// CloneCollectionFiles copies filesystem data from source to destination.
func CloneCollectionFiles(ctx context.Context, srcFS, destFS FileSystem, collectionID string) (int64, error) {
	var totalBytes int64

	// List all files for this collection
	files, err := srcFS.List(ctx, collectionID)
	if err != nil {
		return 0, fmt.Errorf("failed to list source files: %w", err)
	}

	// Copy each file
	for _, filePath := range files {
		// Read from source
		content, err := srcFS.Load(ctx, filePath)
		if err != nil {
			return totalBytes, fmt.Errorf("failed to load file %s: %w", filePath, err)
		}

		// Write to destination
		if err := destFS.Save(ctx, filePath, content); err != nil {
			return totalBytes, fmt.Errorf("failed to save file %s: %w", filePath, err)
		}

		totalBytes += int64(len(content))
	}

	return totalBytes, nil
}

// EstimateCollectionSize estimates the total size of a collection including files.
func EstimateCollectionSize(ctx context.Context, c *Collection, includeFiles bool) (int64, error) {
	var totalSize int64

	// Get database size (approximate - actual size may vary)
	// We'd need to add a method to Store interface for this
	// For now, return a placeholder
	totalSize += 1024 * 1024 // Estimate 1MB for database

	if includeFiles && c.FS != nil {
		// Get filesystem size
		files, err := c.FS.List(ctx, "")
		if err == nil {
			for _, file := range files {
				size, err := c.FS.Stat(ctx, file)
				if err != nil {
					continue // Skip files we can't stat
				}
				totalSize += size
			}
		}
	}

	return totalSize, nil
}
