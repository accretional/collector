package collection

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// LocalFileSystem implements the FileSystem interface using the os package.
type LocalFileSystem struct {
	Root string
}

func (l *LocalFileSystem) Save(ctx context.Context, path string, content []byte) error {
	fullPath := filepath.Join(l.Root, path)
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, content, 0644)
}

func (l *LocalFileSystem) Load(ctx context.Context, path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(l.Root, path))
}

func (l *LocalFileSystem) Delete(ctx context.Context, path string) error {
	return os.Remove(filepath.Join(l.Root, path))
}

func (l *LocalFileSystem) List(ctx context.Context, prefix string) ([]string, error) {
	var files []string
	err := filepath.Walk(l.Root, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			rel, _ := filepath.Rel(l.Root, path)
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}

func (l *LocalFileSystem) Stat(ctx context.Context, path string) (int64, error) {
	info, err := os.Stat(filepath.Join(l.Root, path))
	if err != nil { return 0, err }
	return info.Size(), nil
}
