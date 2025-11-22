package collection

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/fs/local"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// ChunkSize for streaming data (1MB)
	ChunkSize = 1024 * 1024
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

		// Clone filesystem if source has files
		if srcCollection.FS != nil {
			bytes, err := CloneCollectionFiles(ctx, srcCollection.FS, destFS, "")
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
		CollectionId:     fmt.Sprintf("%s/%s", req.DestNamespace, req.DestName),
		RecordsCloned:    recordCount,
		FilesCloned:      fileCount,
		BytesTransferred: bytesTransferred,
	}, nil
}

// CloneRemote clones a collection to a remote collector using streaming.
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

	// Pack the collection for transport
	reader, size, err := cm.transport.Pack(ctx, srcCollection, req.IncludeFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to pack collection: %w", err)
	}
	defer reader.Close()

	// Open streaming RPC
	stream, err := remoteRepoClient.PushCollection(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open push stream: %w", err)
	}

	// Send metadata first
	metadataMsg := &pb.PushCollectionRequest{
		Data: &pb.PushCollectionRequest_Metadata_{
			Metadata: &pb.PushCollectionRequest_Metadata{
				SourceCollection: req.SourceCollection,
				DestNamespace:    req.DestNamespace,
				DestName:         req.DestName,
				IncludeFiles:     req.IncludeFiles,
				TotalSize:        size,
				MessageType:      srcCollection.Meta.MessageType,
			},
		},
	}

	if err := stream.Send(metadataMsg); err != nil {
		return nil, fmt.Errorf("failed to send metadata: %w", err)
	}

	// Stream data in chunks
	buf := make([]byte, ChunkSize)
	totalSent := int64(0)

	for {
		n, err := reader.Read(buf)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read data: %w", err)
		}
		if n == 0 {
			break
		}

		chunkMsg := &pb.PushCollectionRequest{
			Data: &pb.PushCollectionRequest_Chunk{
				Chunk: buf[:n],
			},
		}

		if err := stream.Send(chunkMsg); err != nil {
			return nil, fmt.Errorf("failed to send chunk: %w", err)
		}

		totalSent += int64(n)

		if err == io.EOF {
			break
		}
	}

	// Close stream and receive response
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, fmt.Errorf("failed to close stream: %w", err)
	}

	// Convert PushCollectionResponse to CloneResponse
	return &pb.CloneResponse{
		Status:           resp.Status,
		CollectionId:     resp.CollectionId,
		RecordsCloned:    resp.RecordsCloned,
		FilesCloned:      resp.FilesCloned,
		BytesTransferred: resp.BytesReceived,
	}, nil
}

// FetchRemote fetches a collection from a remote collector using streaming.
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

	remoteRepoClient := pb.NewCollectionRepoClient(conn)

	// Open pull stream
	pullReq := &pb.PullCollectionRequest{
		SourceCollection: req.SourceCollection,
		IncludeFiles:     req.IncludeFiles,
	}

	stream, err := remoteRepoClient.PullCollection(ctx, pullReq)
	if err != nil {
		return nil, fmt.Errorf("failed to open pull stream: %w", err)
	}

	// Receive first message (metadata)
	firstMsg, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("failed to receive metadata: %w", err)
	}

	metadata := firstMsg.GetMetadata()
	if metadata == nil {
		return nil, fmt.Errorf("expected metadata in first message")
	}

	// Create temporary file for receiving data
	destDBPath := filepath.Join(cm.dataDir, "collections", req.DestNamespace, req.DestName+".db")
	if err := os.MkdirAll(filepath.Dir(destDBPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	tmpFile := destDBPath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(tmpFile)
		}
	}()

	// Receive and write data chunks
	totalReceived := int64(0)
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to receive chunk: %w", err)
		}

		chunk := msg.GetChunk()
		if chunk == nil {
			continue
		}

		n, err := f.Write(chunk)
		if err != nil {
			return nil, fmt.Errorf("failed to write chunk: %w", err)
		}
		totalReceived += int64(n)
	}

	// Close temp file
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	// Rename to final location
	if err := os.Rename(tmpFile, destDBPath); err != nil {
		return nil, fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Get remote collection metadata for creating local entry
	routeResp, err := remoteRepoClient.Route(ctx, &pb.RouteRequest{
		Collection: req.SourceCollection,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get collection metadata: %w", err)
	}

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
		os.Remove(destDBPath)
		return nil, fmt.Errorf("failed to create collection metadata: %w", err)
	}

	return &pb.FetchResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: "Collection fetched successfully",
		},
		CollectionId:     fmt.Sprintf("%s/%s", req.DestNamespace, req.DestName),
		RecordsFetched:   metadata.RecordCount,
		FilesFetched:     metadata.FileCount,
		BytesTransferred: totalReceived,
	}, nil
}

// ReceivePushedCollection handles incoming collection push streams (server-side).
func (cm *CloneManager) ReceivePushedCollection(stream pb.CollectionRepo_PushCollectionServer) error {
	ctx := stream.Context()

	// Receive first message (metadata)
	firstMsg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("failed to receive metadata: %w", err)
	}

	metadata := firstMsg.GetMetadata()
	if metadata == nil {
		return fmt.Errorf("expected metadata in first message")
	}

	// Create destination paths
	destDBPath := filepath.Join(cm.dataDir, "collections", metadata.DestNamespace, metadata.DestName+".db")
	if err := os.MkdirAll(filepath.Dir(destDBPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	tmpFile := destDBPath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(tmpFile)
		}
	}()

	// Receive and write data chunks
	totalReceived := int64(0)
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to receive chunk: %w", err)
		}

		chunk := msg.GetChunk()
		if chunk == nil {
			continue
		}

		n, err := f.Write(chunk)
		if err != nil {
			return fmt.Errorf("failed to write chunk: %w", err)
		}
		totalReceived += int64(n)
	}

	// Close and rename temp file
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpFile, destDBPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Count records and files from the unpacked collection
	// For now, we'll use placeholder values
	recordCount := int64(0)
	fileCount := int64(0)

	// Create collection metadata in repository using message type from source
	destMeta := &pb.Collection{
		Namespace:   metadata.DestNamespace,
		Name:        metadata.DestName,
		MessageType: metadata.MessageType,
		Metadata: &pb.Metadata{
			Labels: map[string]string{
				"cloned_from": fmt.Sprintf("%s/%s",
					metadata.SourceCollection.Namespace,
					metadata.SourceCollection.Name),
			},
		},
	}

	_, err = cm.repo.CreateCollection(ctx, destMeta)
	if err != nil {
		os.Remove(destDBPath)
		return fmt.Errorf("failed to create collection metadata: %w", err)
	}

	// Send response
	resp := &pb.PushCollectionResponse{
		Status: &pb.Status{
			Code:    pb.Status_OK,
			Message: "Collection received successfully",
		},
		CollectionId:  fmt.Sprintf("%s/%s", metadata.DestNamespace, metadata.DestName),
		RecordsCloned: recordCount,
		FilesCloned:   fileCount,
		BytesReceived: totalReceived,
	}

	return stream.SendAndClose(resp)
}

// StreamCollectionToPuller handles outgoing collection pull streams (server-side).
func (cm *CloneManager) StreamCollectionToPuller(req *pb.PullCollectionRequest, stream pb.CollectionRepo_PullCollectionServer) error {
	ctx := stream.Context()

	// Get source collection
	srcCollection, err := cm.repo.GetCollection(ctx, req.SourceCollection.Namespace, req.SourceCollection.Name)
	if err != nil {
		return fmt.Errorf("failed to get source collection: %w", err)
	}

	// Count records
	records, err := srcCollection.Store.ListRecords(ctx, 999999, 0)
	if err != nil {
		return fmt.Errorf("failed to count records: %w", err)
	}
	recordCount := int64(len(records))

	// Count files
	fileCount := int64(0)
	if req.IncludeFiles && srcCollection.FS != nil {
		files, err := srcCollection.FS.List(ctx, "")
		if err == nil {
			fileCount = int64(len(files))
		}
	}

	// Pack the collection
	reader, totalSize, err := cm.transport.Pack(ctx, srcCollection, req.IncludeFiles)
	if err != nil {
		return fmt.Errorf("failed to pack collection: %w", err)
	}
	defer reader.Close()

	// Send metadata first
	metadataMsg := &pb.PullCollectionChunk{
		Data: &pb.PullCollectionChunk_Metadata_{
			Metadata: &pb.PullCollectionChunk_Metadata{
				CollectionId: fmt.Sprintf("%s/%s", req.SourceCollection.Namespace, req.SourceCollection.Name),
				TotalSize:    totalSize,
				RecordCount:  recordCount,
				FileCount:    fileCount,
			},
		},
	}

	if err := stream.Send(metadataMsg); err != nil {
		return fmt.Errorf("failed to send metadata: %w", err)
	}

	// Stream data in chunks
	buf := make([]byte, ChunkSize)
	for {
		n, err := reader.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read data: %w", err)
		}
		if n == 0 {
			break
		}

		chunkMsg := &pb.PullCollectionChunk{
			Data: &pb.PullCollectionChunk_Chunk{
				Chunk: buf[:n],
			},
		}

		if err := stream.Send(chunkMsg); err != nil {
			return fmt.Errorf("failed to send chunk: %w", err)
		}

		if err == io.EOF {
			break
		}
	}

	return nil
}
