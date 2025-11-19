package collection

import (
    "bytes"
    "os"
    "path/filepath"
    "testing"

    pb "github.com/accretional/collector/gen/collector"
)

func TestSaveFile_Success(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    data := &pb.CollectionData{
        Name: "test.txt",
        Content: &pb.CollectionData_Data{
            Data: []byte("Hello, World!"),
        },
    }

    err := coll.SaveFile("test.txt", data)
    if err != nil {
        t.Fatalf("failed to save file: %v", err)
    }

    // Verify file exists
    fullPath := filepath.Join(coll.filesPath, "test.txt")
    if _, err := os.Stat(fullPath); os.IsNotExist(err) {
        t.Error("file was not created")
    }

    // Verify content
    content, err := os.ReadFile(fullPath)
    if err != nil {
        t.Fatalf("failed to read file: %v", err)
    }

    if !bytes.Equal(content, []byte("Hello, World!")) {
        t.Errorf("file content mismatch: got '%s'", string(content))
    }
}

func TestSaveFile_InSubdirectory(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    data := &pb.CollectionData{
        Name: "config.json",
        Content: &pb.CollectionData_Data{
            Data: []byte(`{"key": "value"}`),
        },
    }

    err := coll.SaveFile("configs/app/config.json", data)
    if err != nil {
        t.Fatalf("failed to save file: %v", err)
    }

    // Verify directory structure was created
    fullPath := filepath.Join(coll.filesPath, "configs", "app", "config.json")
    if _, err := os.Stat(fullPath); os.IsNotExist(err) {
        t.Error("file was not created in subdirectory")
    }

    // Verify content
    content, err := os.ReadFile(fullPath)
    if err != nil {
        t.Fatalf("failed to read file: %v", err)
    }

    if string(content) != `{"key": "value"}` {
        t.Errorf("file content mismatch: got '%s'", string(content))
    }
}

func TestSaveFile_WithURI(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // First save a source file
    sourceData := &pb.CollectionData{
        Name: "source.txt",
        Content: &pb.CollectionData_Data{
            Data: []byte("source content"),
        },
    }

    if err := coll.SaveFile("source.txt", sourceData); err != nil {
        t.Fatalf("failed to save source file: %v", err)
    }

    // Now save a file with URI reference
    refData := &pb.CollectionData{
        Name: "reference.txt",
        Content: &pb.CollectionData_Uri{
            Uri: "source.txt",
        },
    }

    if err := coll.SaveFile("reference.txt", refData); err != nil {
        t.Fatalf("failed to save reference file: %v", err)
    }

    // Verify reference file has same content as source
    refPath := filepath.Join(coll.filesPath, "reference.txt")
    content, err := os.ReadFile(refPath)
    if err != nil {
        t.Fatalf("failed to read reference file: %v", err)
    }

    if string(content) != "source content" {
        t.Errorf("reference file content mismatch: got '%s'", string(content))
    }
}

func TestGetFile_Success(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Save a file first
    originalData := []byte("test content")
    data := &pb.CollectionData{
        Name: "get_test.txt",
        Content: &pb.CollectionData_Data{
            Data: originalData,
        },
    }

    if err := coll.SaveFile("get_test.txt", data); err != nil {
        t.Fatalf("failed to save file: %v", err)
    }

    // Retrieve it
    retrieved, err := coll.GetFile("get_test.txt")
    if err != nil {
        t.Fatalf("failed to get file: %v", err)
    }

    if retrieved.Name != "get_test.txt" {
        t.Errorf("expected name 'get_test.txt', got '%s'", retrieved.Name)
    }

    // Extract content
    var content []byte
    switch v := retrieved.Content.(type) {
    case *pb.CollectionData_Data:
        content = v.Data
    case *pb.CollectionData_Uri:
        t.Error("expected inline data, got URI")
    }

    if !bytes.Equal(content, originalData) {
        t.Errorf("content mismatch: got '%s'", string(content))
    }
}

func TestGetFile_NotFound(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    _, err := coll.GetFile("nonexistent.txt")
    if err == nil {
        t.Error("expected error for non-existent file, got nil")
    }
}

func TestDeleteFile_Success(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Save a file
    data := &pb.CollectionData{
        Name: "delete_test.txt",
        Content: &pb.CollectionData_Data{
            Data: []byte("to be deleted"),
        },
    }

    if err := coll.SaveFile("delete_test.txt", data); err != nil {
        t.Fatalf("failed to save file: %v", err)
    }

    // Delete it
    if err := coll.DeleteFile("delete_test.txt"); err != nil {
        t.Fatalf("failed to delete file: %v", err)
    }

    // Verify it's gone
    fullPath := filepath.Join(coll.filesPath, "delete_test.txt")
    if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
        t.Error("file still exists after delete")
    }
}

func TestDeleteFile_NotFound(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    err := coll.DeleteFile("nonexistent.txt")
    if err == nil {
        t.Error("expected error for deleting non-existent file, got nil")
    }
}

func TestListFiles_Empty(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    files, err := coll.ListFiles()
    if err != nil {
        t.Fatalf("failed to list files: %v", err)
    }

    if len(files) != 0 {
        t.Errorf("expected 0 files, got %d", len(files))
    }
}

func TestListFiles_Multiple(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create multiple files
    filePaths := []string{
        "file1.txt",
        "dir1/file2.txt",
        "dir1/dir2/file3.txt",
        "dir3/file4.txt",
    }

    for _, path := range filePaths {
        data := &pb.CollectionData{
            Name: filepath.Base(path),
            Content: &pb.CollectionData_Data{
                Data: []byte("content of " + path),
            },
        }
        if err := coll.SaveFile(path, data); err != nil {
            t.Fatalf("failed to save file %s: %v", path, err)
        }
    }

    // List all files
    files, err := coll.ListFiles()
    if err != nil {
        t.Fatalf("failed to list files: %v", err)
    }

    if len(files) != len(filePaths) {
        t.Errorf("expected %d files, got %d", len(filePaths), len(files))
    }

    // Verify all paths are present
    foundPaths := make(map[string]bool)
    for _, file := range files {
        foundPaths[file] = true
    }

    for _, expectedPath := range filePaths {
        if !foundPaths[expectedPath] {
            t.Errorf("file %s not found in list", expectedPath)
        }
    }
}

func TestSaveDir_Success(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create a directory structure
    dir := &pb.CollectionDir{
        Name: "root",
        Subdirs: map[string]*pb.CollectionDir{
            "subdir1": {
                Name: "subdir1",
                Files: map[string]*pb.CollectionData{
                    "file1.txt": {
                        Name: "file1.txt",
                        Content: &pb.CollectionData_Data{
                            Data: []byte("content 1"),
                        },
                    },
                },
            },
        },
        Files: map[string]*pb.CollectionData{
            "root.txt": {
                Name: "root.txt",
                Content: &pb.CollectionData_Data{
                    Data: []byte("root content"),
                },
            },
        },
    }

    if err := coll.SaveDir(dir, ""); err != nil {
        t.Fatalf("failed to save directory: %v", err)
    }

    // Verify structure was created
    rootFile := filepath.Join(coll.filesPath, "root", "root.txt")
    if _, err := os.Stat(rootFile); os.IsNotExist(err) {
        t.Error("root file not created")
    }

    subFile := filepath.Join(coll.filesPath, "root", "subdir1", "file1.txt")
    if _, err := os.Stat(subFile); os.IsNotExist(err) {
        t.Error("subdirectory file not created")
    }

    // Verify content
    content, _ := os.ReadFile(rootFile)
    if string(content) != "root content" {
        t.Errorf("root file content mismatch: got '%s'", string(content))
    }

    content, _ = os.ReadFile(subFile)
    if string(content) != "content 1" {
        t.Errorf("subdir file content mismatch: got '%s'", string(content))
    }
}

func TestLoadDir_Success(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create a directory structure on disk
    dirs := []string{
        filepath.Join(coll.filesPath, "dir1"),
        filepath.Join(coll.filesPath, "dir1", "dir2"),
    }

    for _, dir := range dirs {
        if err := os.MkdirAll(dir, 0755); err != nil {
            t.Fatalf("failed to create directory: %v", err)
        }
    }

    files := map[string]string{
        filepath.Join(coll.filesPath, "file1.txt"):             "content 1",
        filepath.Join(coll.filesPath, "dir1", "file2.txt"):     "content 2",
        filepath.Join(coll.filesPath, "dir1", "dir2", "file3.txt"): "content 3",
    }

    for path, content := range files {
        if err := os.WriteFile(path, []byte(content), 0644); err != nil {
            t.Fatalf("failed to write file: %v", err)
        }
    }

    // Load the directory structure
    dir, err := coll.loadDir(coll.filesPath, "")
    if err != nil {
        t.Fatalf("failed to load directory: %v", err)
    }

    // Verify structure
    if dir.Name != "root" {
        t.Errorf("expected root dir name 'root', got '%s'", dir.Name)
    }

    if len(dir.Files) != 1 {
        t.Errorf("expected 1 file in root, got %d", len(dir.Files))
    }

    if len(dir.Subdirs) != 1 {
        t.Errorf("expected 1 subdir in root, got %d", len(dir.Subdirs))
    }

    // Check subdir
    subdir1, ok := dir.Subdirs["dir1"]
    if !ok {
        t.Fatal("dir1 not found in subdirs")
    }

    if len(subdir1.Files) != 1 {
        t.Errorf("expected 1 file in dir1, got %d", len(subdir1.Files))
    }

    if len(subdir1.Subdirs) != 1 {
        t.Errorf("expected 1 subdir in dir1, got %d", len(subdir1.Subdirs))
    }
}

func TestLoadFilesystem_Integration(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create some files
    files := map[string]string{
        "readme.md":            "# README",
        "docs/guide.md":        "# Guide",
        "docs/api/index.md":    "# API",
    }

    for path, content := range files {
        data := &pb.CollectionData{
            Name: filepath.Base(path),
            Content: &pb.CollectionData_Data{
                Data: []byte(content),
            },
        }
        if err := coll.SaveFile(path, data); err != nil {
            t.Fatalf("failed to save file %s: %v", path, err)
        }
    }

    // Load filesystem structure
    if err := coll.loadFilesystem(); err != nil {
        t.Fatalf("failed to load filesystem: %v", err)
    }

    // Verify structure is loaded into proto
    if coll.proto.Dir == nil {
        t.Fatal("directory structure not loaded into proto")
    }

    // Check root files
    if _, ok := coll.proto.Dir.Files["readme.md"]; !ok {
        t.Error("readme.md not found in root files")
    }

    // Check subdirectory
    docs, ok := coll.proto.Dir.Subdirs["docs"]
    if !ok {
        t.Fatal("docs subdir not found")
    }

    if _, ok := docs.Files["guide.md"]; !ok {
        t.Error("guide.md not found in docs")
    }

    // Check nested subdirectory
    api, ok := docs.Subdirs["api"]
    if !ok {
        t.Fatal("api subdir not found")
    }

    if _, ok := api.Files["index.md"]; !ok {
        t.Error("index.md not found in api")
    }
}

func TestFileSize_SmallVsLarge(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Small file (< 1MB) - should be stored inline
    smallContent := make([]byte, 1024) // 1KB
    for i := range smallContent {
        smallContent[i] = 'a'
    }

    smallData := &pb.CollectionData{
        Name: "small.txt",
        Content: &pb.CollectionData_Data{
            Data: smallContent,
        },
    }

    if err := coll.SaveFile("small.txt", smallData); err != nil {
        t.Fatalf("failed to save small file: %v", err)
    }

    retrieved, err := coll.GetFile("small.txt")
    if err != nil {
        t.Fatalf("failed to get small file: %v", err)
    }

    // Should be inline data
    if _, ok := retrieved.Content.(*pb.CollectionData_Data); !ok {
        t.Error("small file should have inline data")
    }

    // Large file (> 1MB) - should be stored as URI
    largeContent := make([]byte, 2*1024*1024) // 2MB
    for i := range largeContent {
        largeContent[i] = 'b'
    }

    // First write it to disk
    largePath := filepath.Join(coll.filesPath, "large.txt")
    if err := os.WriteFile(largePath, largeContent, 0644); err != nil {
        t.Fatalf("failed to write large file: %v", err)
    }

    // Load it
    largeRetrieved, err := coll.loadFile(largePath, "large.txt")
    if err != nil {
        t.Fatalf("failed to load large file: %v", err)
    }

    // Should be URI reference
    if _, ok := largeRetrieved.Content.(*pb.CollectionData_Uri); !ok {
        t.Error("large file should have URI reference")
    }
}

func TestFlattenJSON(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    tests := []struct {
        name     string
        input    map[string]interface{}
        expected []string
    }{
        {
            name: "simple values",
            input: map[string]interface{}{
                "name":  "Alice",
                "age":   30,
                "active": true,
            },
            expected: []string{"Alice", "30", "true"},
        },
        {
            name: "nested object",
            input: map[string]interface{}{
                "user": map[string]interface{}{
                    "name": "Bob",
                    "email": "bob@example.com",
                },
            },
            expected: []string{"Bob", "bob@example.com"},
        },
        {
            name: "array",
            input: map[string]interface{}{
                "tags": []interface{}{"go", "programming", "database"},
            },
            expected: []string{"go", "programming", "database"},
        },
        {
            name: "nested array of objects",
            input: map[string]interface{}{
                "items": []interface{}{
                    map[string]interface{}{"name": "item1"},
                    map[string]interface{}{"name": "item2"},
                },
            },
            expected: []string{"item1", "item2"},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := coll.flattenJSON(tt.input)
            
            // Check that all expected strings are in the result
            for _, expected := range tt.expected {
                if !contains(result, expected) {
                    t.Errorf("expected '%s' in flattened result, got: %s", expected, result)
                }
            }
        })
    }
}

// Helper function
func contains(str, substr string) bool {
    return len(str) >= len(substr) && (str == substr || len(str) > len(substr) && 
        (str[:len(substr)] == substr || contains(str[1:], substr)))
}
