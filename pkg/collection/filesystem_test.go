package collection_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
)

func TestSaveFile_Success(t *testing.T) {
	coll, cleanup := setupTestCollection(t)
	defer cleanup()
	ctx := context.Background()

	data := &pb.CollectionData{
		Name: "test.txt",
		Content: &pb.CollectionData_Data{
			Data: []byte("Hello, World!"),
		},
	}

	err := coll.SaveFile(ctx, "test.txt", data)
	if err != nil {
		t.Fatalf("failed to save file: %v", err)
	}

	// Retrieve and Verify
	// We use GetFile to verify the internal logic, but we can also check disk
	retrieved, err := coll.GetFile(ctx, "test.txt")
	if err != nil {
		t.Fatalf("failed to retrieve file: %v", err)
	}
	
	content := retrieved.Content.(*pb.CollectionData_Data).Data
	if !bytes.Equal(content, []byte("Hello, World!")) {
		t.Errorf("file content mismatch: got '%s'", string(content))
	}
}

func TestSaveFile_InSubdirectory(t *testing.T) {
	coll, cleanup := setupTestCollection(t)
	defer cleanup()
	ctx := context.Background()

	data := &pb.CollectionData{
		Name: "config.json",
		Content: &pb.CollectionData_Data{
			Data: []byte(`{"key": "value"}`),
		},
	}

	err := coll.SaveFile(ctx, "configs/app/config.json", data)
	if err != nil {
		t.Fatalf("failed to save file: %v", err)
	}

	// Verify retrieval
	retrieved, err := coll.GetFile(ctx, "configs/app/config.json")
	if err != nil {
		t.Fatalf("failed to retrieve file: %v", err)
	}

	content := retrieved.Content.(*pb.CollectionData_Data).Data
	if string(content) != `{"key": "value"}` {
		t.Errorf("file content mismatch: got '%s'", string(content))
	}
}

func TestGetFile_NotFound(t *testing.T) {
	coll, cleanup := setupTestCollection(t)
	defer cleanup()
	ctx := context.Background()

	_, err := coll.GetFile(ctx, "nonexistent.txt")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestDeleteFile_Success(t *testing.T) {
	coll, cleanup := setupTestCollection(t)
	defer cleanup()
	ctx := context.Background()

	// Save a file
	data := &pb.CollectionData{
		Name: "delete_test.txt",
		Content: &pb.CollectionData_Data{
			Data: []byte("to be deleted"),
		},
	}

	if err := coll.SaveFile(ctx, "delete_test.txt", data); err != nil {
		t.Fatalf("failed to save file: %v", err)
	}

	// Delete it
	if err := coll.DeleteFile(ctx, "delete_test.txt"); err != nil {
		t.Fatalf("failed to delete file: %v", err)
	}

	// Verify it's gone
	_, err := coll.GetFile(ctx, "delete_test.txt")
	if err == nil {
		t.Error("file should not exist after delete")
	}
}

func TestListFiles_Multiple(t *testing.T) {
	coll, cleanup := setupTestCollection(t)
	defer cleanup()
	ctx := context.Background()

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
		if err := coll.SaveFile(ctx, path, data); err != nil {
			t.Fatalf("failed to save file %s: %v", path, err)
		}
	}

	// List all files
	files, err := coll.ListFiles(ctx, "")
	if err != nil {
		t.Fatalf("failed to list files: %v", err)
	}

	if len(files) != len(filePaths) {
		t.Errorf("expected %d files, got %d", len(filePaths), len(files))
	}
}

func TestSaveDir_Success(t *testing.T) {
	coll, cleanup := setupTestCollection(t)
	defer cleanup()
	ctx := context.Background()

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

	if err := coll.SaveDir(ctx, dir, ""); err != nil {
		t.Fatalf("failed to save directory: %v", err)
	}

	// Verify files were created via GetFile
	_, err := coll.GetFile(ctx, "root/root.txt")
	if err != nil { t.Error("root file not created") }

	_, err = coll.GetFile(ctx, "root/subdir1/file1.txt")
	if err != nil { t.Error("subdir file not created") }
}

func TestFileSize_SmallVsLarge(t *testing.T) {
	coll, cleanup := setupTestCollection(t)
	defer cleanup()
	ctx := context.Background()

	// Small file (< 1MB) - should be returned inline
	smallContent := make([]byte, 1024) // 1KB
	for i := range smallContent { smallContent[i] = 'a' }

	smallData := &pb.CollectionData{
		Name: "small.txt",
		Content: &pb.CollectionData_Data{ Data: smallContent },
	}
	if err := coll.SaveFile(ctx, "small.txt", smallData); err != nil {
		t.Fatal(err)
	}

	retrieved, _ := coll.GetFile(ctx, "small.txt")
	if _, ok := retrieved.Content.(*pb.CollectionData_Data); !ok {
		t.Error("small file should have inline data")
	}

	// Large file (> 1MB) - we simulate this by writing directly to disk
	// because SaveFile implementation forces inline currently.
	// The test is for GetFile's logic.
	
	// Need to access FS root to write manually
	// Since FS is an interface, we can't assume implementation details here easily
	// BUT, assuming the GetFile implementation logic:
	
	// Let's create a large file using SaveFile (it will save to disk)
	largeContent := make([]byte, 2*1024*1024) // 2MB
	for i := range largeContent { largeContent[i] = 'b' }

	largeData := &pb.CollectionData{
		Name: "large.txt",
		Content: &pb.CollectionData_Data{ Data: largeContent },
	}
	if err := coll.SaveFile(ctx, "large.txt", largeData); err != nil {
		t.Fatal(err)
	}

	// Now Retrieve it. The GetFile logic I implemented checks size.
	// If size > 1MB, it returns URI.
	largeRetrieved, err := coll.GetFile(ctx, "large.txt")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := largeRetrieved.Content.(*pb.CollectionData_Uri); !ok {
		t.Error("large file should have URI reference")
	}
	
	uri := largeRetrieved.Content.(*pb.CollectionData_Uri).Uri
	// On Windows/Unix path separators might differ, check suffix
	if filepath.Base(uri) != "large.txt" {
		t.Errorf("unexpected uri: %s", uri)
	}
}
