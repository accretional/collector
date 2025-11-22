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
	repo CollectionRepo
}

// NewGrpcServer creates a new instance of our gRPC server.
func NewGrpcServer(repo CollectionRepo) *GrpcServer {
	return &GrpcServer{repo: repo}
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
