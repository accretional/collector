package collection

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	pb "github.com/accretional/collector/gen/collector"
)

// Options configures the feature set for a Collection.
type Options struct {
	EnableFTS        bool
	EnableJSON       bool
	EnableVector     bool
	VectorDimensions int
}

// Collection is the domain entity handling logic.
type Collection struct {
	Meta  *pb.Collection
	Store Store
	FS    FileSystem
}

// NewCollection initializes a Collection.
func NewCollection(meta *pb.Collection, store Store, fs FileSystem) (*Collection, error) {
	if meta.Namespace == "" || meta.Name == "" {
		return nil, fmt.Errorf("namespace and name are required")
	}

	if meta.Metadata == nil {
		now := time.Now().Unix()
		meta.Metadata = &pb.Metadata{
			CreatedAt: &pb.Timestamp{Seconds: now},
			UpdatedAt: &pb.Timestamp{Seconds: now},
		}
	}

	return &Collection{
		Meta:  meta,
		Store: store,
		FS:    fs,
	}, nil
}

// --- Store Delegates ---

func (c *Collection) CreateRecord(ctx context.Context, record *pb.CollectionRecord) error {
	if record.Id == "" { return fmt.Errorf("record id required") }
	// Ensure metadata exists
	if record.Metadata == nil { record.Metadata = &pb.Metadata{} }
	if record.Metadata.CreatedAt == nil {
		now := time.Now().Unix()
		record.Metadata.CreatedAt = &pb.Timestamp{Seconds: now}
		record.Metadata.UpdatedAt = &pb.Timestamp{Seconds: now}
	}
	return c.Store.CreateRecord(ctx, record)
}

func (c *Collection) GetRecord(ctx context.Context, id string) (*pb.CollectionRecord, error) {
	return c.Store.GetRecord(ctx, id)
}

func (c *Collection) UpdateRecord(ctx context.Context, record *pb.CollectionRecord) error {
	if record.Id == "" { return fmt.Errorf("record id required") }
	record.Metadata.UpdatedAt = &pb.Timestamp{Seconds: time.Now().Unix()}
	return c.Store.UpdateRecord(ctx, record)
}

func (c *Collection) DeleteRecord(ctx context.Context, id string) error {
	return c.Store.DeleteRecord(ctx, id)
}

func (c *Collection) ListRecords(ctx context.Context, offset, limit int) ([]*pb.CollectionRecord, error) {
	return c.Store.ListRecords(ctx, offset, limit)
}

func (c *Collection) CountRecords(ctx context.Context) (int64, error) {
	return c.Store.CountRecords(ctx)
}

func (c *Collection) Search(ctx context.Context, query *SearchQuery) ([]*SearchResult, error) {
	return c.Store.Search(ctx, query)
}

func (c *Collection) Checkpoint(ctx context.Context) error {
	return c.Store.Checkpoint(ctx)
}

func (c *Collection) Close() error {
	return c.Store.Close()
}

func (c *Collection) GetNamespace() string { return c.Meta.Namespace }
func (c *Collection) GetName() string { return c.Meta.Name }

// --- Filesystem Logic ---

// SaveFile writes a CollectionData proto to the underlying FileSystem.
func (c *Collection) SaveFile(ctx context.Context, path string, data *pb.CollectionData) error {
	var content []byte

	switch v := data.Content.(type) {
	case *pb.CollectionData_Data:
		content = v.Data
	case *pb.CollectionData_Uri:
		return fmt.Errorf("saving URI references directly not supported in this implementation")
	default:
		return fmt.Errorf("unknown content type")
	}

	return c.FS.Save(ctx, path, content)
}

// GetFile retrieves a file. It automatically handles the logic of 
// returning raw bytes for small files or a URI for large files (optional optimization).
func (c *Collection) GetFile(ctx context.Context, path string) (*pb.CollectionData, error) {
	// 1. Check size
	size, err := c.FS.Stat(ctx, path)
	if err != nil {
		return nil, err
	}

	data := &pb.CollectionData{
		Name: filepath.Base(path),
	}

	// Threshold: 1MB
	if size < 1024*1024 {
		content, err := c.FS.Load(ctx, path)
		if err != nil {
			return nil, err
		}
		data.Content = &pb.CollectionData_Data{Data: content}
	} else {
		// For large files, return the path as a URI relative to the root
		data.Content = &pb.CollectionData_Uri{Uri: path}
	}

	return data, nil
}

func (c *Collection) DeleteFile(ctx context.Context, path string) error {
	return c.FS.Delete(ctx, path)
}

func (c *Collection) ListFiles(ctx context.Context, prefix string) ([]string, error) {
	return c.FS.List(ctx, prefix)
}

// SaveDir recursively saves a CollectionDir structure.
func (c *Collection) SaveDir(ctx context.Context, dir *pb.CollectionDir, parentPath string) error {
	// 1. Save Files
	for name, file := range dir.Files {
		filePath := filepath.Join(parentPath, dir.Name, name)
		if err := c.SaveFile(ctx, filePath, file); err != nil {
			return err
		}
	}

	// 2. Recurse Subdirs
	for _, subdir := range dir.Subdirs {
		subdirParent := filepath.Join(parentPath, dir.Name)
		if err := c.SaveDir(ctx, subdir, subdirParent); err != nil {
			return err
		}
	}
	return nil
}
