package collection

import (
	"context"
	"fmt"
	"strings"
	"sync"

	pb "github.com/accretional/collector/gen/collector"
)

// CollectionRepoService provides a persistent implementation of the CollectionRepo interface.
// It uses a Store (like SqliteStore) for the underlying data storage.
type CollectionRepoService struct {
	store       Store
	collections map[string]*pb.Collection // Track created collections by namespace/name
	mu          sync.RWMutex
}

// NewCollectionRepoService creates a new service instance.
func NewCollectionRepoService(store Store) *CollectionRepoService {
	return &CollectionRepoService{
		store:       store,
		collections: make(map[string]*pb.Collection),
	}
}

// CreateCollection creates a new collection.
func (s *CollectionRepoService) CreateCollection(ctx context.Context, collection *pb.Collection) (*pb.CreateCollectionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate input
	if collection == nil {
		return nil, fmt.Errorf("collection cannot be nil")
	}

	// For simplicity, we'll use the collection's name as its ID.
	// In a real-world scenario, you'd likely generate a unique ID.
	id := fmt.Sprintf("%s/%s", collection.Namespace, collection.Name)

	// Check if collection already exists
	if _, exists := s.collections[id]; exists {
		return nil, fmt.Errorf("collection %s already exists", id)
	}

	// Track the collection
	s.collections[id] = collection

	return &pb.CreateCollectionResponse{
		Status:       &pb.Status{Code: 200, Message: "OK"},
		CollectionId: id,
	}, nil
}

// Discover finds collections based on the provided criteria.
func (s *CollectionRepoService) Discover(ctx context.Context, req *pb.DiscoverRequest) (*pb.DiscoverResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []*pb.Collection

	// Filter collections based on criteria
	for _, coll := range s.collections {
		// Filter by namespace
		if req.Namespace != "" && coll.Namespace != req.Namespace {
			continue
		}

		// Filter by message type
		if req.MessageTypeFilter != nil {
			if coll.MessageType == nil ||
				coll.MessageType.MessageName != req.MessageTypeFilter.MessageName {
				continue
			}
		}

		// Filter by labels
		if len(req.LabelFilter) > 0 {
			if coll.Metadata == nil || coll.Metadata.Labels == nil {
				continue
			}
			matches := true
			for key, value := range req.LabelFilter {
				if coll.Metadata.Labels[key] != value {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
		}

		matched = append(matched, coll)
	}

	// Apply pagination
	pageSize := int(req.PageSize)
	if pageSize == 0 {
		pageSize = 100 // Default page size
	}

	offset := 0
	if req.PageToken != "" {
		// Simple pagination: page token is just the offset as a string
		fmt.Sscanf(req.PageToken, "%d", &offset)
	}

	// Calculate end index
	end := offset + pageSize
	if end > len(matched) {
		end = len(matched)
	}

	// Get paginated results
	var results []*pb.Collection
	if offset < len(matched) {
		results = matched[offset:end]
	}

	// Generate next page token
	var nextPageToken string
	if end < len(matched) {
		nextPageToken = fmt.Sprintf("%d", end)
	}

	return &pb.DiscoverResponse{
		Status:        &pb.Status{Code: 200, Message: "OK"},
		Collections:   results,
		NextPageToken: nextPageToken,
	}, nil
}

// Route directs a request to the appropriate collection server.
func (s *CollectionRepoService) Route(ctx context.Context, req *pb.RouteRequest) (*pb.RouteResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Validate input
	if req.Collection == nil {
		return &pb.RouteResponse{
			Status: &pb.Status{Code: 400, Message: "collection is required"},
		}, nil
	}

	// Look up the collection
	id := fmt.Sprintf("%s/%s", req.Collection.Namespace, req.Collection.Name)
	coll, exists := s.collections[id]
	if !exists {
		return &pb.RouteResponse{
			Status: &pb.Status{Code: 404, Message: fmt.Sprintf("collection %s not found", id)},
		}, nil
	}

	// Return the server endpoint
	endpoint := coll.ServerEndpoint
	if endpoint == "" {
		// Default to a local endpoint if not specified
		endpoint = "localhost:50051"
	}

	return &pb.RouteResponse{
		Status:         &pb.Status{Code: 200, Message: "OK"},
		ServerEndpoint: endpoint,
		Collection:     coll,
	}, nil
}

// SearchCollections searches across multiple collections.
func (s *CollectionRepoService) SearchCollections(ctx context.Context, req *pb.SearchCollectionsRequest) (*pb.SearchCollectionsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Determine which collections to search
	var collectionsToSearch []*pb.Collection

	if len(req.CollectionNames) > 0 {
		// Search specific collections
		for _, name := range req.CollectionNames {
			id := fmt.Sprintf("%s/%s", req.Namespace, name)
			if coll, exists := s.collections[id]; exists {
				collectionsToSearch = append(collectionsToSearch, coll)
			}
		}
	} else {
		// Search all collections in the namespace (or all if namespace is empty)
		for id, coll := range s.collections {
			if req.Namespace == "" || strings.HasPrefix(id, req.Namespace+"/") {
				collectionsToSearch = append(collectionsToSearch, coll)
			}
		}
	}

	// For now, return a placeholder response indicating which collections would be searched
	// A full implementation would:
	// 1. Create Collection instances for each collection
	// 2. Convert req.Query (structpb.Struct) to SearchQuery
	// 3. Execute searches across all collections
	// 4. Aggregate and rank results
	// 5. Apply pagination

	collectionIds := make([]string, len(collectionsToSearch))
	for i, coll := range collectionsToSearch {
		collectionIds[i] = fmt.Sprintf("%s/%s", coll.Namespace, coll.Name)
	}

	// Return empty results with metadata about what would be searched
	return &pb.SearchCollectionsResponse{
		Status: &pb.Status{
			Code:    200,
			Message: fmt.Sprintf("Would search %d collections: %s", len(collectionsToSearch), strings.Join(collectionIds, ", ")),
		},
		Results:      []*pb.SearchCollectionsResponse_CollectionResult{},
		TotalMatches: 0,
	}, nil
}
