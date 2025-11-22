package collection

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
)

func TestCloneCollectionFiles_Simple(t *testing.T) {
	ctx := context.Background()

	// Create temp directories
	tempDir, err := os.MkdirTemp("", "clone-files-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	srcDir := filepath.Join(tempDir, "source")
	destDir := filepath.Join(tempDir, "dest")

	// Create source filesystem
	srcFS, err := NewLocalFileSystem(srcDir)
	if err != nil {
		t.Fatalf("failed to create source filesystem: %v", err)
	}

	// Create destination filesystem
	destFS, err := NewLocalFileSystem(destDir)
	if err != nil {
		t.Fatalf("failed to create dest filesystem: %v", err)
	}

	// Add test files to source
	testFiles := map[string][]byte{
		"file1.txt":     []byte("content 1"),
		"dir/file2.txt": []byte("content 2"),
		"dir/file3.txt": []byte("content 3"),
	}

	for path, content := range testFiles {
		if err := srcFS.Save(ctx, path, content); err != nil {
			t.Fatalf("failed to save test file %s: %v", path, err)
		}
	}

	// Clone files
	bytesTransferred, err := CloneCollectionFiles(ctx, srcFS, destFS, "")
	if err != nil {
		t.Fatalf("CloneCollectionFiles failed: %v", err)
	}

	if bytesTransferred == 0 {
		t.Error("expected non-zero bytes transferred")
	}

	// Verify files were cloned
	for path, expectedContent := range testFiles {
		content, err := destFS.Load(ctx, path)
		if err != nil {
			t.Errorf("failed to load cloned file %s: %v", path, err)
			continue
		}

		if string(content) != string(expectedContent) {
			t.Errorf("file %s: expected content %q, got %q", path, expectedContent, content)
		}
	}

	// Verify file count
	files, err := destFS.List(ctx, "")
	if err != nil {
		t.Fatalf("failed to list files: %v", err)
	}

	if len(files) != len(testFiles) {
		t.Errorf("expected %d files, got %d", len(testFiles), len(files))
	}
}

func TestCloneManager_ValidateRequest(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clone-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a mock repo
	mockRepo := &mockCollectionRepo{}
	cloneManager := NewCloneManager(mockRepo, tempDir)

	ctx := context.Background()

	t.Run("CloneLocal_MissingSourceCollection", func(t *testing.T) {
		req := &pb.CloneRequest{
			DestNamespace: "test-ns",
			DestName:      "clone-dest",
		}

		_, err := cloneManager.CloneLocal(ctx, req)
		if err == nil {
			t.Error("expected error for missing source collection")
		}
	})

	t.Run("CloneLocal_MissingDestination", func(t *testing.T) {
		req := &pb.CloneRequest{
			SourceCollection: &pb.NamespacedName{
				Namespace: "test-ns",
				Name:      "source",
			},
			// Missing DestNamespace and DestName
		}

		_, err := cloneManager.CloneLocal(ctx, req)
		if err == nil {
			t.Error("expected error for missing destination")
		}
	})

	t.Run("FetchRemote_MissingSourceEndpoint", func(t *testing.T) {
		req := &pb.FetchRequest{
			SourceCollection: &pb.NamespacedName{
				Namespace: "test-ns",
				Name:      "source",
			},
			DestNamespace: "test-ns",
			DestName:      "dest",
		}

		_, err := cloneManager.FetchRemote(ctx, req)
		if err == nil {
			t.Error("expected error for missing source endpoint")
		}
	})

	t.Run("FetchRemote_MissingSourceCollection", func(t *testing.T) {
		req := &pb.FetchRequest{
			SourceEndpoint: "localhost:50051",
			DestNamespace:  "test-ns",
			DestName:       "dest",
		}

		_, err := cloneManager.FetchRemote(ctx, req)
		if err == nil {
			t.Error("expected error for missing source collection")
		}
	})
}

// mockCollectionRepo is a minimal mock for testing
type mockCollectionRepo struct {
	collections map[string]*Collection
}

func (m *mockCollectionRepo) CreateCollection(ctx context.Context, collection *pb.Collection) (*pb.CreateCollectionResponse, error) {
	return &pb.CreateCollectionResponse{
		Status:       &pb.Status{Code: pb.Status_OK},
		CollectionId: collection.Namespace + "/" + collection.Name,
	}, nil
}

func (m *mockCollectionRepo) Discover(ctx context.Context, req *pb.DiscoverRequest) (*pb.DiscoverResponse, error) {
	return &pb.DiscoverResponse{
		Status: &pb.Status{Code: pb.Status_OK},
	}, nil
}

func (m *mockCollectionRepo) Route(ctx context.Context, req *pb.RouteRequest) (*pb.RouteResponse, error) {
	return &pb.RouteResponse{
		Status: &pb.Status{Code: pb.Status_OK},
	}, nil
}

func (m *mockCollectionRepo) SearchCollections(ctx context.Context, req *pb.SearchCollectionsRequest) (*pb.SearchCollectionsResponse, error) {
	return &pb.SearchCollectionsResponse{
		Status: &pb.Status{Code: pb.Status_OK},
	}, nil
}

func (m *mockCollectionRepo) GetCollection(ctx context.Context, namespace, name string) (*Collection, error) {
	return nil, nil
}

func (m *mockCollectionRepo) UpdateCollectionMetadata(ctx context.Context, namespace, name string, meta *pb.Collection) error {
	return nil
}
