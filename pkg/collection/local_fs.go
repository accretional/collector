package collection

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// LocalFileSystem implements the FileSystem interface using os package.
type LocalFileSystem struct {
	Root string
}

func (l *LocalFileSystem) Save(ctx context.Context, path string, content []byte) error {
	fullPath := filepath.Join(l.Root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, content, 0644)
}

func (l *LocalFileSystem) Load(ctx context.Context, path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(l.Root, path))
}

// ... Implement Delete, List, Stat similarly ...
func (l *LocalFileSystem) Delete(ctx context.Context, path string) error {
	return os.Remove(filepath.Join(l.Root, path))
}
func (l *LocalFileSystem) List(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil // Simplified for brevity
}
func (l *LocalFileSystem) Stat(ctx context.Context, path string) (int64, error) {
	info, err := os.Stat(filepath.Join(l.Root, path))
	if err != nil { return 0, err }
	return info.Size(), nil
}
