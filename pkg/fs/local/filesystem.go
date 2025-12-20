package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FileSystem implements file operations using the local OS filesystem.
type FileSystem struct {
	Root string
}

// NewFileSystem creates a new local filesystem rooted at the given path.
func NewFileSystem(root string) (*FileSystem, error) {
	// Ensure root directory exists
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("failed to create root directory: %w", err)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	return &FileSystem{Root: absRoot}, nil
}

// sanitizePath validates and cleans a user-provided path to prevent directory traversal attacks.
// Returns error if the path attempts to escape the root directory or contains suspicious patterns.
func (fs *FileSystem) sanitizePath(path string) (string, error) {
	// Clean the path to remove any ".." or "." components
	cleaned := filepath.Clean(path)

	// Reject absolute paths
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("absolute paths not allowed: %s", path)
	}

	// Reject paths that still contain ".." after cleaning
	// This catches attempts like "../../etc/passwd"
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("path traversal detected: %s", path)
	}

	// Build the full path and ensure it's within our root
	fullPath := filepath.Join(fs.Root, cleaned)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	// Ensure the resolved path is still within our root directory
	if !strings.HasPrefix(absPath, fs.Root+string(filepath.Separator)) && absPath != fs.Root {
		return "", fmt.Errorf("path escapes root directory: %s", path)
	}

	return cleaned, nil
}

// Save writes content to a file at the given path.
func (fs *FileSystem) Save(ctx context.Context, path string, content []byte) error {
	// Sanitize path to prevent directory traversal
	cleanPath, err := fs.sanitizePath(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	fullPath := filepath.Join(fs.Root, cleanPath)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write to temp file first, then atomic rename
	tmpPath := fullPath + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tmpPath, fullPath); err != nil {
		os.Remove(tmpPath) // Clean up temp file
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// Load reads content from a file at the given path.
func (fs *FileSystem) Load(ctx context.Context, path string) ([]byte, error) {
	// Sanitize path to prevent directory traversal
	cleanPath, err := fs.sanitizePath(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	fullPath := filepath.Join(fs.Root, cleanPath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return content, nil
}

// Delete removes a file at the given path.
func (fs *FileSystem) Delete(ctx context.Context, path string) error {
	// Sanitize path to prevent directory traversal
	cleanPath, err := fs.sanitizePath(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	fullPath := filepath.Join(fs.Root, cleanPath)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

// List returns all files under the given prefix.
func (fs *FileSystem) List(ctx context.Context, prefix string) ([]string, error) {
	// Sanitize prefix to prevent directory traversal
	cleanPrefix, err := fs.sanitizePath(prefix)
	if err != nil {
		return nil, fmt.Errorf("invalid prefix: %w", err)
	}

	var files []string
	searchPath := filepath.Join(fs.Root, cleanPrefix)

	err = filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip errors for individual files/dirs
			return nil
		}

		if !info.IsDir() {
			rel, err := filepath.Rel(fs.Root, path)
			if err != nil {
				return nil // Skip if we can't get relative path
			}
			files = append(files, rel)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return files, nil
}

// Stat returns the size of a file at the given path.
func (fs *FileSystem) Stat(ctx context.Context, path string) (int64, error) {
	// Sanitize path to prevent directory traversal
	cleanPath, err := fs.sanitizePath(path)
	if err != nil {
		return 0, fmt.Errorf("invalid path: %w", err)
	}

	fullPath := filepath.Join(fs.Root, cleanPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return 0, fmt.Errorf("failed to stat file: %w", err)
	}
	return info.Size(), nil
}

// SaveDir recursively copies a directory from srcPath on local filesystem to destPath in this filesystem.
func (fs *FileSystem) SaveDir(ctx context.Context, destPath, srcPath string) error {
	return filepath.Walk(srcPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Get relative path from source
		relPath, err := filepath.Rel(srcPath, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read source file: %w", err)
		}

		// Save to destination
		destFilePath := filepath.Join(destPath, relPath)
		return fs.Save(ctx, destFilePath, content)
	})
}

// CopyFile copies a file from srcPath to destPath within this filesystem.
func (fs *FileSystem) CopyFile(ctx context.Context, srcPath, destPath string) error {
	// Path sanitization is done by Load and Save
	content, err := fs.Load(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("failed to load source: %w", err)
	}

	return fs.Save(ctx, destPath, content)
}

// MoveFile moves a file from srcPath to destPath within this filesystem.
func (fs *FileSystem) MoveFile(ctx context.Context, srcPath, destPath string) error {
	// Sanitize both paths to prevent directory traversal
	cleanSrc, err := fs.sanitizePath(srcPath)
	if err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	cleanDest, err := fs.sanitizePath(destPath)
	if err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	srcFull := filepath.Join(fs.Root, cleanSrc)
	destFull := filepath.Join(fs.Root, cleanDest)

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destFull), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	if err := os.Rename(srcFull, destFull); err != nil {
		return fmt.Errorf("failed to move file: %w", err)
	}

	return nil
}

// Exists checks if a file exists at the given path.
func (fs *FileSystem) Exists(ctx context.Context, path string) (bool, error) {
	// Sanitize path to prevent directory traversal
	cleanPath, err := fs.sanitizePath(path)
	if err != nil {
		return false, fmt.Errorf("invalid path: %w", err)
	}

	fullPath := filepath.Join(fs.Root, cleanPath)
	_, err = os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check existence: %w", err)
}

// OpenReader opens a file for reading.
func (fs *FileSystem) OpenReader(ctx context.Context, path string) (io.ReadCloser, error) {
	// Sanitize path to prevent directory traversal
	cleanPath, err := fs.sanitizePath(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	fullPath := filepath.Join(fs.Root, cleanPath)
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	return file, nil
}

// OpenWriter opens a file for writing.
func (fs *FileSystem) OpenWriter(ctx context.Context, path string) (io.WriteCloser, error) {
	// Sanitize path to prevent directory traversal
	cleanPath, err := fs.sanitizePath(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	fullPath := filepath.Join(fs.Root, cleanPath)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	file, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	return file, nil
}
