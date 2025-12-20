package collection

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

	// Create tar archive
	tarPath := filepath.Join(tmpDir, "collection.tar")
	tarFile, err := os.Create(tarPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create tar file: %w", err)
	}

	tw := tar.NewWriter(tarFile)

	// Add database to tar
	if err := addFileToTar(tw, dbPath, "collection.db"); err != nil {
		tw.Close()
		tarFile.Close()
		return nil, 0, fmt.Errorf("failed to add database to tar: %w", err)
	}

	// Add files if requested
	if includeFiles && c.FS != nil {
		files, err := c.FS.List(ctx, "")
		if err != nil {
			tw.Close()
			tarFile.Close()
			return nil, 0, fmt.Errorf("failed to list files: %w", err)
		}

		for _, filePath := range files {
			content, err := c.FS.Load(ctx, filePath)
			if err != nil {
				tw.Close()
				tarFile.Close()
				return nil, 0, fmt.Errorf("failed to load file %s: %w", filePath, err)
			}

			// Add file to tar with "files/" prefix
			tarPath := filepath.Join("files", filePath)
			if err := addContentToTar(tw, content, tarPath); err != nil {
				tw.Close()
				tarFile.Close()
				return nil, 0, fmt.Errorf("failed to add file %s to tar: %w", filePath, err)
			}
		}
	}

	// Close tar writer to flush
	if err := tw.Close(); err != nil {
		tarFile.Close()
		return nil, 0, fmt.Errorf("failed to close tar writer: %w", err)
	}

	// Close and reopen for reading
	if err := tarFile.Close(); err != nil {
		return nil, 0, fmt.Errorf("failed to close tar file: %w", err)
	}

	// Open for reading
	file, err := os.Open(tarPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open tar file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, fmt.Errorf("failed to stat tar file: %w", err)
	}

	return file, stat.Size(), nil
}

// Unpack receives collection data and creates a new collection.
// If the data is a tar archive, it extracts the database and files.
// Otherwise, it treats the data as a raw database file.
func (t *SqliteTransport) Unpack(ctx context.Context, reader io.Reader, destPath string) error {
	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Try to read as tar archive first
	tr := tar.NewReader(reader)
	header, err := tr.Next()

	// If not a tar archive, treat as raw database file
	if err == io.EOF {
		return fmt.Errorf("empty archive")
	}
	if err != nil {
		// Not a tar archive - might be raw database
		// Write directly to destination
		tmpPath := destPath + ".tmp"
		tmpFile, err := os.Create(tmpPath)
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		defer os.Remove(tmpPath)

		if _, err := io.Copy(tmpFile, reader); err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write data: %w", err)
		}

		if err := tmpFile.Close(); err != nil {
			return fmt.Errorf("failed to close temp file: %w", err)
		}

		if err := os.Rename(tmpPath, destPath); err != nil {
			return fmt.Errorf("failed to rename to destination: %w", err)
		}

		return nil
	}

	// It's a tar archive - extract it
	var foundDB bool
	filesDir := filepath.Join(filepath.Dir(destPath), "files")

	for {
		if header == nil {
			break
		}

		targetPath := ""
		if header.Name == "collection.db" {
			// Extract database to destPath
			targetPath = destPath + ".tmp"
			foundDB = true
		} else if strings.HasPrefix(header.Name, "files/") {
			// Extract files to files directory
			relPath := header.Name[6:] // Remove "files/" prefix
			targetPath = filepath.Join(filesDir, relPath)

			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create directory for %s: %w", header.Name, err)
			}
		}

		if targetPath != "" {
			// Extract the file
			outFile, err := os.Create(targetPath)
			if err != nil {
				return fmt.Errorf("failed to create %s: %w", targetPath, err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to write %s: %w", header.Name, err)
			}

			if err := outFile.Close(); err != nil {
				return fmt.Errorf("failed to close %s: %w", targetPath, err)
			}
		}

		// Read next header
		header, err = tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}
	}

	if !foundDB {
		return fmt.Errorf("database file not found in archive")
	}

	// Rename database to final location
	tmpDBPath := destPath + ".tmp"
	if err := os.Rename(tmpDBPath, destPath); err != nil {
		return fmt.Errorf("failed to rename database to destination: %w", err)
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

// addFileToTar adds a file from disk to a tar archive.
func addFileToTar(tw *tar.Writer, sourcePath, tarPath string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	header := &tar.Header{
		Name:    tarPath,
		Size:    stat.Size(),
		Mode:    int64(stat.Mode()),
		ModTime: stat.ModTime(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	if _, err := io.Copy(tw, file); err != nil {
		return fmt.Errorf("failed to write file content: %w", err)
	}

	return nil
}

// addContentToTar adds content from memory to a tar archive.
func addContentToTar(tw *tar.Writer, content []byte, tarPath string) error {
	header := &tar.Header{
		Name: tarPath,
		Size: int64(len(content)),
		Mode: 0644,
	}

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	if _, err := tw.Write(content); err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	return nil
}
