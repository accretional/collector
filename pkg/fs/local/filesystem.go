package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// Save writes content to a file at the given path.
func (fs *FileSystem) Save(ctx context.Context, path string, content []byte) error {
	fullPath := filepath.Join(fs.Root, path)

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
	fullPath := filepath.Join(fs.Root, path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return content, nil
}

// Delete removes a file at the given path.
func (fs *FileSystem) Delete(ctx context.Context, path string) error {
	fullPath := filepath.Join(fs.Root, path)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

// List returns all files under the given prefix.
func (fs *FileSystem) List(ctx context.Context, prefix string) ([]string, error) {
	var files []string
	searchPath := filepath.Join(fs.Root, prefix)

	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
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
	fullPath := filepath.Join(fs.Root, path)
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
	content, err := fs.Load(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("failed to load source: %w", err)
	}

	return fs.Save(ctx, destPath, content)
}

// MoveFile moves a file from srcPath to destPath within this filesystem.
func (fs *FileSystem) MoveFile(ctx context.Context, srcPath, destPath string) error {
	srcFull := filepath.Join(fs.Root, srcPath)
	destFull := filepath.Join(fs.Root, destPath)

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
	fullPath := filepath.Join(fs.Root, path)
	_, err := os.Stat(fullPath)
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
	fullPath := filepath.Join(fs.Root, path)
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	return file, nil
}

// OpenWriter opens a file for writing.
func (fs *FileSystem) OpenWriter(ctx context.Context, path string) (io.WriteCloser, error) {
	fullPath := filepath.Join(fs.Root, path)

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
