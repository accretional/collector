package collection

import (
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"

    pb "github.com/accretional/collector/gen/collector"
)

// loadFilesystem loads the CollectionDir structure from disk
func (c *Collection) loadFilesystem() error {
    if _, err := os.Stat(c.filesPath); os.IsNotExist(err) {
        // No filesystem structure exists yet
        return nil
    }

    dir, err := c.loadDir(c.filesPath, "")
    if err != nil {
        return err
    }

    c.proto.Dir = dir
    return nil
}

// loadDir recursively loads a directory structure
func (c *Collection) loadDir(fsPath, relativePath string) (*pb.CollectionDir, error) {
    entries, err := os.ReadDir(fsPath)
    if err != nil {
        return nil, err
    }

    dir := &pb.CollectionDir{
        Name:    filepath.Base(relativePath),
        Subdirs: make(map[string]*pb.CollectionDir),
        Files:   make(map[string]*pb.CollectionData),
    }

    if dir.Name == "" {
        dir.Name = "root"
    }

    for _, entry := range entries {
        entryPath := filepath.Join(fsPath, entry.Name())
        entryRelPath := filepath.Join(relativePath, entry.Name())

        if entry.IsDir() {
            subdir, err := c.loadDir(entryPath, entryRelPath)
            if err != nil {
                return nil, err
            }
            dir.Subdirs[entry.Name()] = subdir
        } else {
            file, err := c.loadFile(entryPath, entry.Name())
            if err != nil {
                return nil, err
            }
            dir.Files[entry.Name()] = file
        }
    }

    return dir, nil
}

// loadFile loads a single file as CollectionData
func (c *Collection) loadFile(fsPath, name string) (*pb.CollectionData, error) {
    info, err := os.Stat(fsPath)
    if err != nil {
        return nil, err
    }

    data := &pb.CollectionData{
        Name: name,
    }

    // For small files (< 1MB), store inline; for larger, store URI reference
    if info.Size() < 1024*1024 {
        bytes, err := os.ReadFile(fsPath)
        if err != nil {
            return nil, err
        }
        data.Content = &pb.CollectionData_Data{Data: bytes}
    } else {
        // Store relative path as URI
        relPath, err := filepath.Rel(c.filesPath, fsPath)
        if err != nil {
            return nil, err
        }
        data.Content = &pb.CollectionData_Uri{Uri: relPath}
    }

    return data, nil
}

// SaveFile saves a CollectionData to the filesystem
func (c *Collection) SaveFile(path string, data *pb.CollectionData) error {
    fullPath := filepath.Join(c.filesPath, path)
    if !strings.HasPrefix(filepath.Clean(fullPath), filepath.Clean(c.filesPath)) {
        return fmt.Errorf("invalid file path: traversal detected")
    }

    // Create parent directories
    if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
        return fmt.Errorf("failed to create directory: %w", err)
    }

    var content []byte
    switch v := data.Content.(type) {
    case *pb.CollectionData_Data:
        content = v.Data
    case *pb.CollectionData_Uri:
        // If URI, read from referenced location
        srcPath := filepath.Join(c.filesPath, v.Uri)
        src, err := os.Open(srcPath)
        if err != nil {
            return fmt.Errorf("failed to open source: %w", err)
        }
        defer src.Close()

        dst, err := os.Create(fullPath)
        if err != nil {
            return fmt.Errorf("failed to create destination: %w", err)
        }
        defer dst.Close()

        _, err = io.Copy(dst, src)
        return err
    default:
        return fmt.Errorf("unknown content type")
    }

    return os.WriteFile(fullPath, content, 0644)
}

// GetFile retrieves a file from the filesystem
func (c *Collection) GetFile(path string) (*pb.CollectionData, error) {
    fullPath := filepath.Join(c.filesPath, path)
    return c.loadFile(fullPath, filepath.Base(path))
}

// DeleteFile removes a file from the filesystem
func (c *Collection) DeleteFile(path string) error {
    fullPath := filepath.Join(c.filesPath, path)
    return os.Remove(fullPath)
}

// SaveDir recursively saves a CollectionDir structure to disk
func (c *Collection) SaveDir(dir *pb.CollectionDir, parentPath string) error {
    dirPath := filepath.Join(c.filesPath, parentPath, dir.Name)
    if err := os.MkdirAll(dirPath, 0755); err != nil {
        return fmt.Errorf("failed to create directory: %w", err)
    }

    // Save files
    for name, file := range dir.Files {
        filePath := filepath.Join(parentPath, dir.Name, name)
        if err := c.SaveFile(filePath, file); err != nil {
            return err
        }
    }

    // Save subdirectories recursively
    for _, subdir := range dir.Subdirs {
        subdirParent := filepath.Join(parentPath, dir.Name)
        if err := c.SaveDir(subdir, subdirParent); err != nil {
            return err
        }
    }

    return nil
}

// ListFiles returns all file paths in the collection
func (c *Collection) ListFiles() ([]string, error) {
    var files []string

    err := filepath.Walk(c.filesPath, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        if !info.IsDir() {
            relPath, err := filepath.Rel(c.filesPath, path)
            if err != nil {
                return err
            }
            files = append(files, relPath)
        }
        return nil
    })

    return files, err
}
