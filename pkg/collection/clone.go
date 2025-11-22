package collection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/fs/local"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// CloneManager handles collection cloning operations.
type CloneManager struct {
	repo      CollectionRepo
	transport Transport
	fetcher   *Fetcher
	dataDir   string
}

// NewCloneManager creates a new CloneManager.
func NewCloneManager(repo CollectionRepo, dataDir string) *CloneManager {
	return &CloneManager{
		repo:      repo,
		transport: &SqliteTransport{},
		fetcher:   NewFetcher(),
		dataDir:   dataDir,
	}
}

// CloneLocal clones a collection within the same collector.
func (cm *CloneManager) CloneLocal(ctx context.Context, req *pb.CloneRequest) (*pb.CloneResponse, error) {
	// Validate request
	if req.SourceCollection == nil {
		return nil, fmt.Errorf("source collection is required")
	}
	if req.DestNamespace == "" || req.DestName == "" {
		return nil, fmt.Errorf("destination namespace and name are required")
	}

	// Get source collection
	srcNamespace := req.SourceCollection.Namespace
	srcName := req.SourceCollection.Name
	srcCollection, err := cm.repo.GetCollection(ctx, srcNamespace, srcName)
	if err != nil {
		return nil, fmt.Errorf("failed to get source collection: %w", err)
	}

	// Create destination paths
	destDBPath := filepath.Join(cm.dataDir, "collections", req.DestNamespace, req.DestName+".db")
	destFilesPath := filepath.Join(cm.dataDir, "files", req.DestNamespace, req.DestName)

	// Clone database
	if err := cm.transport.Clone(ctx, srcCollection, destDBPath); err != nil {
		return nil, fmt.Errorf("failed to clone database: %w", err)
	}

	// Count records from source collection (they're the same in the clone)
	srcRecords, err := srcCollection.Store.ListRecords(ctx, 999999, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to count records: %w", err)
	}
	recordCount := int64(len(srcRecords))

	// Clone files if requested
	var fileCount int64
	var bytesTransferred int64
	if req.IncludeFiles && srcCollection.FS != nil {
		// Create destination filesystem
		destFS, err := local.NewFileSystem(destFilesPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create destination filesystem: %w", err)
		}

		// Get source filesystem
		srcFS, ok := srcCollection.FS.(*LocalFileSystem)
		if ok && srcFS.fs != nil {
			bytes, err := CloneCollectionFiles(ctx, srcFS.fs, destFS, "")
			if err != nil {
				return nil, fmt.Errorf("failed to clone files: %w", err)
			}
			bytesTransferred = bytes

			// Count files
			files, err := destFS.List(ctx, "")
			if err == nil {
				fileCount = int64(len(files))
			}
		}
	}

	// Create collection metadata in repository
	destMeta := &pb.Collection{
		Namespace:   req.DestNamespace,
		Name:        req.DestName,
		MessageType: srcCollection.Meta.MessageType,
		Metadata: &pb.Metadata{
			Labels: map[string]string{
				"cloned_from": fmt.Sprintf("%s/%s", srcNamespace, srcName),
			},
		},
	}

	_, err = cm.repo.CreateCollection(ctx, destMeta)
	if err != nil {
		// Clean up on failure
		os.Remove(destDBPath)
		os.RemoveAll(destFilesPath)
		return nil, fmt.Errorf("failed to create collection metadata: %w", err)
	}

	return &pb.CloneResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: "Collection cloned successfully",
		},
		CollectionId:      fmt.Sprintf("%s/%s", req.DestNamespace, req.DestName),
		RecordsCloned:     recordCount,
		FilesCloned:       fileCount,
		BytesTransferred:  bytesTransferred,
	}, nil
}

// CloneRemote clones a collection to a remote collector.
func (cm *CloneManager) CloneRemote(ctx context.Context, req *pb.CloneRequest) (*pb.CloneResponse, error) {
	// Validate request
	if req.SourceCollection == nil {
		return nil, fmt.Errorf("source collection is required")
	}
	if req.DestEndpoint == "" {
		return nil, fmt.Errorf("destination endpoint is required for remote clone")
	}

	// Get source collection
	srcCollection, err := cm.repo.GetCollection(ctx, req.SourceCollection.Namespace, req.SourceCollection.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get source collection: %w", err)
	}

	// Connect to remote collector
	conn, err := grpc.NewClient(req.DestEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to remote collector: %w", err)
	}
	defer conn.Close()

	remoteRepoClient := pb.NewCollectionRepoClient(conn)

	// Create a fetch request for the remote side
	fetchReq := &pb.FetchRequest{
		SourceEndpoint:     "current", // Special marker for local collection
		SourceCollection:   req.SourceCollection,
		DestNamespace:      req.DestNamespace,
		DestName:           req.DestName,
		IncludeFiles:       req.IncludeFiles,
	}

	// Pack the collection for transport
	reader, size, err := cm.transport.Pack(ctx, srcCollection, req.IncludeFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to pack collection: %w", err)
	}
	defer reader.Close()

	// TODO: Implement actual remote cloning
	// For now, return a placeholder response
	_ = fetchReq
	_ = remoteRepoClient

	return &pb.CloneResponse{
		Status: &pb.Status{
			Code:    pb.Status_UNIMPLEMENTED,
			Message: "Remote cloning not yet fully implemented",
		},
		CollectionId:     fmt.Sprintf("%s/%s", req.DestNamespace, req.DestName),
		RecordsCloned:    0,
		FilesCloned:      0,
		BytesTransferred: size,
	}, nil
}

// FetchRemote fetches a collection from a remote collector.
func (cm *CloneManager) FetchRemote(ctx context.Context, req *pb.FetchRequest) (*pb.FetchResponse, error) {
	// Validate request
	if req.SourceEndpoint == "" {
		return nil, fmt.Errorf("source endpoint is required")
	}
	if req.SourceCollection == nil {
		return nil, fmt.Errorf("source collection is required")
	}
	if req.DestNamespace == "" || req.DestName == "" {
		return nil, fmt.Errorf("destination namespace and name are required")
	}

	// Connect to remote collector
	conn, err := grpc.NewClient(req.SourceEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to remote collector: %w", err)
	}
	defer conn.Close()

	// Get remote collection metadata
	remoteRepoClient := pb.NewCollectionRepoClient(conn)
	routeResp, err := remoteRepoClient.Route(ctx, &pb.RouteRequest{
		Collection: req.SourceCollection,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to route to remote collection: %w", err)
	}

	if routeResp.Status.Code != pb.Status_OK {
		return nil, fmt.Errorf("remote collection not found: %s", routeResp.Status.Message)
	}

	// Create local paths
	destDBPath := filepath.Join(cm.dataDir, "collections", req.DestNamespace, req.DestName+".db")
	_ = destDBPath // TODO: Use this when implementing actual fetch

	// In a real implementation, we would stream the data
	// For now, we'll create a placeholder implementation
	// TODO: Implement actual streaming fetch from remote

	// Create destination collection metadata
	destMeta := &pb.Collection{
		Namespace:   req.DestNamespace,
		Name:        req.DestName,
		MessageType: routeResp.Collection.MessageType,
		Metadata: &pb.Metadata{
			Labels: map[string]string{
				"fetched_from": fmt.Sprintf("%s/%s@%s",
					req.SourceCollection.Namespace,
					req.SourceCollection.Name,
					req.SourceEndpoint),
			},
		},
	}

	_, err = cm.repo.CreateCollection(ctx, destMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to create collection metadata: %w", err)
	}

	return &pb.FetchResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: "Collection fetch initiated (placeholder implementation)",
		},
		CollectionId:      fmt.Sprintf("%s/%s", req.DestNamespace, req.DestName),
		RecordsFetched:    0, // TODO: Actual count
		FilesFetched:      0, // TODO: Actual count
		BytesTransferred:  0, // TODO: Actual count
	}, nil
}
