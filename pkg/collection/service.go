package collection

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"

	pb "github.com/accretional/collector/gen/collector"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// CollectionRepoService provides a persistent implementation of the CollectionRepo interface.
// It uses a Store (like SqliteStore) for the underlying data storage.
type CollectionRepoService struct {
	store         Store
	registryStore RegistryStore             // Persist collection metadata
	collections   map[string]*pb.Collection // In-memory cache for performance
	mu            sync.RWMutex
}

// NewCollectionRepoService creates a new service instance.
func NewCollectionRepoService(store Store, registryStore RegistryStore) *CollectionRepoService {
	service := &CollectionRepoService{
		store:         store,
		registryStore: registryStore,
		collections:   make(map[string]*pb.Collection),
	}
	// Load existing collections from registry into in-memory cache
	service.loadFromRegistry(context.Background())
	return service
}

// CreateCollection creates a new collection.
func (s *CollectionRepoService) CreateCollection(ctx context.Context, collection *pb.Collection) (*pb.CreateCollectionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate input
	if collection == nil {
		return nil, fmt.Errorf("collection cannot be nil")
	}

	// Validate namespace and collection name
	if err := ValidateNamespace(collection.Namespace); err != nil {
		return nil, fmt.Errorf("invalid namespace: %w", err)
	}
	if err := ValidateCollectionName(collection.Name); err != nil {
		return nil, fmt.Errorf("invalid collection name: %w", err)
	}

	// For simplicity, we'll use the collection's name as its ID.
	// In a real-world scenario, you'd likely generate a unique ID.
	id := fmt.Sprintf("%s/%s", collection.Namespace, collection.Name)

	// Check if collection already exists
	if _, exists := s.collections[id]; exists {
		return nil, fmt.Errorf("collection %s already exists", id)
	}

	// Track the collection in-memory
	s.collections[id] = collection

	// Persist to registry store if available
	if s.registryStore != nil {
		dbPath := fmt.Sprintf("%s/%s.db", collection.Namespace, collection.Name)
		if err := s.registryStore.SaveCollection(ctx, collection, dbPath); err != nil {
			// Rollback in-memory
			delete(s.collections, id)
			return nil, fmt.Errorf("failed to persist collection to registry: %w", err)
		}
	}

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

type searchResultWithMeta struct {
	collectionName  string
	result          *SearchResult
	normalizedScore float64
}

func parseStructToSearchQuery(s *structpb.Struct, limit int) (*SearchQuery, error) {
	query := &SearchQuery{
		Limit: limit,
	}

	if s == nil {
		return query, nil
	}

	fields := s.GetFields()

	if v, ok := fields["full_text"]; ok {
		query.FullText = v.GetStringValue()
	}

	if v, ok := fields["vector"]; ok {
		if listVal := v.GetListValue(); listVal != nil {
			for _, item := range listVal.GetValues() {
				query.Vector = append(query.Vector, float32(item.GetNumberValue()))
			}
		}
	}

	if v, ok := fields["similarity_threshold"]; ok {
		query.SimilarityThreshold = float32(v.GetNumberValue())
	}

	if v, ok := fields["filters"]; ok {
		filters, err := parseFiltersFromStruct(v)
		if err != nil {
			return nil, fmt.Errorf("invalid filters: %w", err)
		}
		query.Filters = filters
	}

	if v, ok := fields["post_filters"]; ok {
		postFilters, err := parseFiltersFromStruct(v)
		if err != nil {
			return nil, fmt.Errorf("invalid post_filters: %w", err)
		}
		query.PostFilters = postFilters
	}

	if v, ok := fields["order_by"]; ok {
		query.OrderBy = v.GetStringValue()
	}

	if v, ok := fields["ascending"]; ok {
		query.Ascending = v.GetBoolValue()
	}

	return query, nil
}

func parseFiltersFromStruct(v *structpb.Value) ([]Filter, error) {
	listVal := v.GetListValue()
	if listVal == nil {
		return nil, nil
	}

	var filters []Filter
	for _, item := range listVal.GetValues() {
		filterStruct := item.GetStructValue()
		if filterStruct == nil {
			continue
		}

		fields := filterStruct.GetFields()
		filter := Filter{}

		if f, ok := fields["field"]; ok {
			filter.Field = f.GetStringValue()
		}

		if op, ok := fields["operator"]; ok {
			filter.Operator = parseFilterOperator(op.GetStringValue())
		}

		if val, ok := fields["value"]; ok {
			filter.Value = structValueToInterface(val)
		}

		filters = append(filters, filter)
	}

	return filters, nil
}

func parseFilterOperator(op string) FilterOperator {
	switch strings.ToUpper(op) {
	case "=", "EQUALS", "EQ":
		return OpEquals
	case "!=", "NOT_EQUALS", "NE":
		return OpNotEquals
	case ">", "GREATER_THAN", "GT":
		return OpGreaterThan
	case "<", "LESS_THAN", "LT":
		return OpLessThan
	case ">=", "GREATER_EQUAL", "GTE":
		return OpGreaterEqual
	case "<=", "LESS_EQUAL", "LTE":
		return OpLessEqual
	case "CONTAINS", "LIKE":
		return OpContains
	case "IN":
		return OpIn
	case "EXISTS":
		return OpExists
	case "NOT_EXISTS":
		return OpNotExists
	default:
		return OpEquals
	}
}

func structValueToInterface(v *structpb.Value) interface{} {
	if v == nil {
		return nil
	}
	switch v.Kind.(type) {
	case *structpb.Value_NullValue:
		return nil
	case *structpb.Value_NumberValue:
		return v.GetNumberValue()
	case *structpb.Value_StringValue:
		return v.GetStringValue()
	case *structpb.Value_BoolValue:
		return v.GetBoolValue()
	default:
		return nil
	}
}

// Normalizes search scores to a 0-1 range for cross-collection ranking.
// For FTS (BM25): scores are negative, lower (more negative) is better
// For Vector (distance): lower distance is better
func normalizeScore(result *SearchResult, hasVector bool) float64 {
	if hasVector {
		if result.Distance >= 0 {
			return 1.0 / (1.0 + result.Distance)
		}
		return 0.0
	}

	if result.Score != 0 {
		return 1.0 / (1.0 + math.Exp(result.Score/5.0))
	}

	return 0.5 // Default score for scalar searches
}

// We need the collection getter from the repo as the service only holds metadata info
type CollectionGetter func(ctx context.Context, namespace, name string) (*Collection, error)

func (s *CollectionRepoService) SearchCollections(
	ctx context.Context,
	req *pb.SearchCollectionsRequest,
	getCollection CollectionGetter,
) (*pb.SearchCollectionsResponse, error) {

	allCollections, err := s.registryStore.ListCollections(ctx, req.Namespace)
	if err != nil {
		return &pb.SearchCollectionsResponse{
			Status: &pb.Status{Code: 500, Message: fmt.Sprintf("failed to list collections: %v", err)},
		}, nil
	}

	var collectionsToSearch []*pb.Collection
	if len(req.CollectionNames) > 0 {
		nameSet := make(map[string]bool)
		for _, name := range req.CollectionNames {
			nameSet[name] = true
		}
		for _, meta := range allCollections {
			if nameSet[meta.Collection.Name] {
				collectionsToSearch = append(collectionsToSearch, meta.Collection)
			}
		}
	} else {
		for _, meta := range allCollections {
			collectionsToSearch = append(collectionsToSearch, meta.Collection)
		}
	}

	if len(collectionsToSearch) == 0 {
		return &pb.SearchCollectionsResponse{
			Status:       &pb.Status{Code: 200, Message: "OK"},
			Results:      []*pb.SearchCollectionsResponse_CollectionResult{},
			TotalMatches: 0,
		}, nil
	}

	globalLimit := int(req.Limit)
	if globalLimit <= 0 {
		globalLimit = 100
	}

	perCollectionLimit := globalLimit * 2
	if perCollectionLimit < 50 {
		perCollectionLimit = 50
	}

	query, err := parseStructToSearchQuery(req.Query, perCollectionLimit)
	if err != nil {
		return &pb.SearchCollectionsResponse{
			Status: &pb.Status{Code: 400, Message: fmt.Sprintf("invalid query: %v", err)},
		}, nil
	}

	hasVector := len(query.Vector) > 0

	// Search collections concurrently
	var (
		allResults []searchResultWithMeta
		resultsMu  sync.Mutex
		errors     []string
		errorsMu   sync.Mutex
	)

	g, gctx := errgroup.WithContext(ctx)

	for _, collMeta := range collectionsToSearch {
		g.Go(func() error {
			coll, err := getCollection(gctx, collMeta.Namespace, collMeta.Name)
			if err != nil {
				errorsMu.Lock()
				errors = append(errors, fmt.Sprintf("%s/%s: %v", collMeta.Namespace, collMeta.Name, err))
				errorsMu.Unlock()
				return nil
			}
			defer coll.Close()

			results, err := coll.Search(gctx, query)
			if err != nil {
				errorsMu.Lock()
				errors = append(errors, fmt.Sprintf("%s/%s: search failed: %v", collMeta.Namespace, collMeta.Name, err))
				errorsMu.Unlock()
				return nil
			}

			collName := fmt.Sprintf("%s/%s", collMeta.Namespace, collMeta.Name)
			resultsMu.Lock()
			for _, res := range results {
				allResults = append(allResults, searchResultWithMeta{
					collectionName:  collName,
					result:          res,
					normalizedScore: normalizeScore(res, hasVector),
				})
			}
			resultsMu.Unlock()

			return nil
		})
	}

	_ = g.Wait()

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].normalizedScore > allResults[j].normalizedScore
	})

	if len(allResults) > globalLimit {
		allResults = allResults[:globalLimit]
	}

	collectionResults := make(map[string]*pb.SearchCollectionsResponse_CollectionResult)
	for _, r := range allResults {
		cr, exists := collectionResults[r.collectionName]
		if !exists {
			cr = &pb.SearchCollectionsResponse_CollectionResult{
				CollectionName: r.collectionName,
				Items:          []*anypb.Any{},
				Scores:         make(map[string]float64),
			}
			collectionResults[r.collectionName] = cr
		}

		item := &anypb.Any{
			TypeUrl: "type.googleapis.com/collector.record",
			Value:   r.result.Record.ProtoData,
		}
		cr.Items = append(cr.Items, item)
		cr.Scores[r.result.Record.Id] = r.normalizedScore
	}

	results := make([]*pb.SearchCollectionsResponse_CollectionResult, 0, len(collectionResults))
	for _, cr := range collectionResults {
		results = append(results, cr)
	}

	statusMsg := "OK"
	if len(errors) > 0 {
		statusMsg = fmt.Sprintf("Partial results. Errors: %v", errors)
	}

	return &pb.SearchCollectionsResponse{
		Status: &pb.Status{
			Code:    200,
			Message: statusMsg,
		},
		Results:      results,
		TotalMatches: int64(len(allResults)),
	}, nil
}

// loadFromRegistry loads existing collections from the registry store into the in-memory cache.
// This is called during initialization to restore state.
func (s *CollectionRepoService) loadFromRegistry(ctx context.Context) {
	if s.registryStore == nil {
		return
	}

	collections, err := s.registryStore.ListCollections(ctx, "")
	if err != nil {
		log.Printf("Warning: failed to load collections from registry: %v", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, meta := range collections {
		id := fmt.Sprintf("%s/%s", meta.Collection.Namespace, meta.Collection.Name)
		s.collections[id] = meta.Collection
	}

	if len(collections) > 0 {
		log.Printf("Loaded %d collections from registry", len(collections))
	}
}
