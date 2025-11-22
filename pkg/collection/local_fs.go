package collection

import (
	"context"

	"github.com/accretional/collector/pkg/fs/local"
)

// LocalFileSystem implements the FileSystem interface.
// This is a compatibility wrapper around pkg/fs/local.FileSystem.
// New code should use pkg/fs/local.FileSystem directly.
type LocalFileSystem struct {
	fs *local.FileSystem
}

// NewLocalFileSystem creates a new LocalFileSystem rooted at the given path.
func NewLocalFileSystem(root string) (*LocalFileSystem, error) {
	fs, err := local.NewFileSystem(root)
	if err != nil {
		return nil, err
	}
	return &LocalFileSystem{fs: fs}, nil
}

func (l *LocalFileSystem) Save(ctx context.Context, path string, content []byte) error {
	return l.fs.Save(ctx, path, content)
}

func (l *LocalFileSystem) Load(ctx context.Context, path string) ([]byte, error) {
	return l.fs.Load(ctx, path)
}

func (l *LocalFileSystem) Delete(ctx context.Context, path string) error {
	return l.fs.Delete(ctx, path)
}

func (l *LocalFileSystem) List(ctx context.Context, prefix string) ([]string, error) {
	return l.fs.List(ctx, prefix)
}

func (l *LocalFileSystem) Stat(ctx context.Context, path string) (int64, error) {
	return l.fs.Stat(ctx, path)
}
