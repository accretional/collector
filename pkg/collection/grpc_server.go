package collection

import (
	"context"
	"fmt"
	"log"
	"net"

	pb "github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc"
)

// GrpcServer wraps the gRPC server and implements the CollectionRepoServer.
type GrpcServer struct {
	pb.UnimplementedCollectionRepoServer
	repo         CollectionRepo
	cloneManager *CloneManager
}

// NewGrpcServer creates a new instance of our gRPC server.
func NewGrpcServer(repo CollectionRepo) *GrpcServer {
	return &GrpcServer{
		repo:         repo,
		cloneManager: NewCloneManager(repo, "./data"),
	}
}

// NewGrpcServerWithDataDir creates a new instance with a custom data directory.
func NewGrpcServerWithDataDir(repo CollectionRepo, dataDir string) *GrpcServer {
	return &GrpcServer{
		repo:         repo,
		cloneManager: NewCloneManager(repo, dataDir),
	}
}

// CreateCollection forwards the request to the underlying repository.
func (s *GrpcServer) CreateCollection(ctx context.Context, req *pb.CreateCollectionRequest) (*pb.CreateCollectionResponse, error) {
	return s.repo.CreateCollection(ctx, req.Collection)
}

// Discover forwards the request to the underlying repository.
func (s *GrpcServer) Discover(ctx context.Context, req *pb.DiscoverRequest) (*pb.DiscoverResponse, error) {
	return s.repo.Discover(ctx, req)
}

// Route forwards the request to the underlying repository.
func (s *GrpcServer) Route(ctx context.Context, req *pb.RouteRequest) (*pb.RouteResponse, error) {
	return s.repo.Route(ctx, req)
}

// SearchCollections forwards the request to the underlying repository.
func (s *GrpcServer) SearchCollections(ctx context.Context, req *pb.SearchCollectionsRequest) (*pb.SearchCollectionsResponse, error) {
	return s.repo.SearchCollections(ctx, req)
}

// Clone clones a collection either locally or to a remote collector.
func (s *GrpcServer) Clone(ctx context.Context, req *pb.CloneRequest) (*pb.CloneResponse, error) {
	// Validate request
	if req == nil {
		return &pb.CloneResponse{
			Status: &pb.Status{
				Code:    pb.Status_INVALID_ARGUMENT,
				Message: "request is required",
			},
		}, nil
	}

	// Route to appropriate implementation
	if req.DestEndpoint == "" {
		// Local clone
		return s.cloneManager.CloneLocal(ctx, req)
	}

	// Remote clone
	return s.cloneManager.CloneRemote(ctx, req)
}

// Fetch fetches a collection from a remote collector.
func (s *GrpcServer) Fetch(ctx context.Context, req *pb.FetchRequest) (*pb.FetchResponse, error) {
	// Validate request
	if req == nil {
		return &pb.FetchResponse{
			Status: &pb.Status{
				Code:    pb.Status_INVALID_ARGUMENT,
				Message: "request is required",
			},
		}, nil
	}

	return s.cloneManager.FetchRemote(ctx, req)
}

// PushCollection receives a streamed collection from a client and creates it locally.
func (s *GrpcServer) PushCollection(stream pb.CollectionRepo_PushCollectionServer) error {
	return s.cloneManager.ReceivePushedCollection(stream)
}

// PullCollection streams a collection to a client.
func (s *GrpcServer) PullCollection(req *pb.PullCollectionRequest, stream pb.CollectionRepo_PullCollectionServer) error {
	return s.cloneManager.StreamCollectionToPuller(req, stream)
}

// Start runs the gRPC server on the given port.
func (s *GrpcServer) Start(port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterCollectionRepoServer(grpcServer, s)
	log.Printf("server listening at %v", lis.Addr())
	return grpcServer.Serve(lis)
}
