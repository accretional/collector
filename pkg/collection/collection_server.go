package collection

import (
	"context"
	"encoding/base64"
	"strconv"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
)

type CollectionServer struct {
	pb.UnimplementedCollectionServiceServer
	repo CollectionRepo
}

func NewCollectionServer(repo CollectionRepo) *CollectionServer {
	return &CollectionServer{
		repo: repo,
	}
}

func (s *CollectionServer) Create(ctx context.Context, req *pb.CreateRequest) (*pb.CreateResponse, error) {
	collection, err := s.repo.GetCollection(ctx, req.Namespace, req.CollectionName)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "collection not found: %v", err)
	}

	id := req.Id
	if id == "" {
		id = uuid.New().String()
	}

	record := &pb.CollectionRecord{
		Id:        id,
		ProtoData: req.Item.Value,
	}

	if err := collection.CreateRecord(ctx, record); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create record: %v", err)
	}

	return &pb.CreateResponse{Id: id}, nil
}

func (s *CollectionServer) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	collection, err := s.repo.GetCollection(ctx, req.Namespace, req.CollectionName)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "collection not found: %v", err)
	}

	record, err := collection.GetRecord(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "record not found: %v", err)
	}

	any := &anypb.Any{
		TypeUrl: "type.googleapis.com/collector." + collection.Meta.MessageType.MessageName,
		Value:   record.ProtoData,
	}

	return &pb.GetResponse{Item: any}, nil
}

func (s *CollectionServer) Update(ctx context.Context, req *pb.UpdateRequest) (*pb.UpdateResponse, error) {
	collection, err := s.repo.GetCollection(ctx, req.Namespace, req.CollectionName)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "collection not found: %v", err)
	}

	record := &pb.CollectionRecord{
		Id:        req.Id,
		ProtoData: req.Item.Value,
	}

	if err := collection.UpdateRecord(ctx, record); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update record: %v", err)
	}

	return &pb.UpdateResponse{}, nil
}

func (s *CollectionServer) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	collection, err := s.repo.GetCollection(ctx, req.Namespace, req.CollectionName)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "collection not found: %v", err)
	}

	if err := collection.DeleteRecord(ctx, req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete record: %v", err)
	}
	return &pb.DeleteResponse{}, nil
}

func (s *CollectionServer) List(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	collection, err := s.repo.GetCollection(ctx, req.Namespace, req.CollectionName)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "collection not found: %v", err)
	}

	offset, err := pageTokenToOffset(req.PageToken)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid page token: %v", err)
	}
	limit := int(req.PageSize)
	if limit == 0 {
		limit = 100
	}

	records, err := collection.ListRecords(ctx, offset, limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list records: %v", err)
	}

	items := make([]*anypb.Any, len(records))
	for i, record := range records {
		items[i] = &anypb.Any{
			TypeUrl: "type.googleapis.com/collector." + collection.Meta.MessageType.MessageName,
			Value:   record.ProtoData,
		}
	}

	var nextPageToken string
	if len(records) == limit {
		nextPageToken = offsetToPageToken(offset + limit)
	}

	return &pb.ListResponse{
		Items:         items,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *CollectionServer) Search(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, error) {
	collection, err := s.repo.GetCollection(ctx, req.Namespace, req.CollectionName)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "collection not found: %v", err)
	}

	query := &SearchQuery{
		FullText:            req.FullText,
		Filters:             make(map[string]Filter),
		LabelFilters:        req.LabelFilters,
		Vector:              req.Vector,
		SimilarityThreshold: req.SimilarityThreshold,
		Limit:               int(req.Limit),
		Offset:              int(req.Offset),
		OrderBy:             req.OrderBy,
		Ascending:           req.Ascending,
	}

	for k, v := range req.Filters {
		var op FilterOperator
		switch v.Operator {
		case pb.FilterOperator_OP_EQUALS:
			op = OpEquals
		case pb.FilterOperator_OP_NOT_EQUALS:
			op = OpNotEquals
		case pb.FilterOperator_OP_GREATER_THAN:
			op = OpGreaterThan
		case pb.FilterOperator_OP_LESS_THAN:
			op = OpLessThan
		case pb.FilterOperator_OP_GREATER_EQUAL:
			op = OpGreaterEqual
		case pb.FilterOperator_OP_LESS_EQUAL:
			op = OpLessEqual
		case pb.FilterOperator_OP_CONTAINS:
			op = OpContains
		case pb.FilterOperator_OP_IN:
			op = OpIn
		case pb.FilterOperator_OP_EXISTS:
			op = OpExists
		case pb.FilterOperator_OP_NOT_EXISTS:
			op = OpNotExists
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported filter operator: %v", v.Operator)
		}
		query.Filters[k] = Filter{
			Operator: op,
			Value:    v.Value,
		}
	}

	results, err := collection.Search(ctx, query)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "search failed: %v", err)
	}

	resp := &pb.SearchResponse{
		Results: make([]*pb.SearchResult, len(results)),
	}
	for i, res := range results {
		resp.Results[i] = &pb.SearchResult{
			Item: &anypb.Any{
				TypeUrl: "type.googleapis.com/collector." + collection.Meta.MessageType.MessageName,
				Value:   res.Record.ProtoData,
			},
			Score:    res.Score,
			Distance: res.Distance,
		}
	}

	return resp, nil
}

func (s *CollectionServer) Batch(ctx context.Context, req *pb.BatchRequest) (*pb.BatchResponse, error) {
	responses := make([]*pb.ResponseOp, 0, len(req.Operations))

	for _, op := range req.Operations {
		var resp *pb.ResponseOp

		switch o := op.Operation.(type) {
		case *pb.RequestOp_Create:
			createResp, createErr := s.Create(ctx, o.Create)
			if createErr != nil {
				s, _ := status.FromError(createErr)
				resp = &pb.ResponseOp{Status: &pb.Status{Code: pb.Status_Code(s.Code()), Message: s.Message()}}
			} else {
				resp = &pb.ResponseOp{
					Status:   &pb.Status{Code: pb.Status_OK},
					Response: &pb.ResponseOp_Create{Create: createResp},
				}
			}
		case *pb.RequestOp_Update:
			updateResp, updateErr := s.Update(ctx, o.Update)
			if updateErr != nil {
				s, _ := status.FromError(updateErr)
				resp = &pb.ResponseOp{Status: &pb.Status{Code: pb.Status_Code(s.Code()), Message: s.Message()}}
			} else {
				resp = &pb.ResponseOp{
					Status:   &pb.Status{Code: pb.Status_OK},
					Response: &pb.ResponseOp_Update{Update: updateResp},
				}
			}
		case *pb.RequestOp_Delete:
			deleteResp, deleteErr := s.Delete(ctx, o.Delete)
			if deleteErr != nil {
				s, _ := status.FromError(deleteErr)
				resp = &pb.ResponseOp{Status: &pb.Status{Code: pb.Status_Code(s.Code()), Message: s.Message()}}
			} else {
				resp = &pb.ResponseOp{
					Status:   &pb.Status{Code: pb.Status_OK},
					Response: &pb.ResponseOp_Delete{Delete: deleteResp},
				}
			}
		}
		responses = append(responses, resp)
	}

	return &pb.BatchResponse{Responses: responses}, nil
}

func (s *CollectionServer) Describe(ctx context.Context, req *pb.DescribeRequest) (*pb.DescribeResponse, error) {
	collection, err := s.repo.GetCollection(ctx, req.Namespace, req.CollectionName)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "collection not found: %v", err)
	}

	count, err := collection.CountRecords(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to count records: %v", err)
	}

	size, err := collection.FS.Stat(ctx, collection.Store.Path())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get storage size: %v", err)
	}

	return &pb.DescribeResponse{
		CollectionDefinition: collection.Meta,
		RecordCount:          count,
		StorageSizeBytes:     size,
	}, nil
}

func (s *CollectionServer) Modify(ctx context.Context, req *pb.ModifyRequest) (*pb.ModifyResponse, error) {
	collection, err := s.repo.GetCollection(ctx, req.Namespace, req.CollectionName)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "collection not found: %v", err)
	}

	collection.Meta.IndexedFields = req.IndexedFields
	if err := collection.Store.ReIndex(ctx); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to re-index: %v", err)
	}
	return &pb.ModifyResponse{}, nil
}

func (s *CollectionServer) Meta(ctx context.Context, req *pb.MetaRequest) (*pb.MetaResponse, error) {
	return &pb.MetaResponse{
		ServerVersion: "0.0.1",
	}, nil
}

func (s *CollectionServer) Invoke(ctx context.Context, req *pb.InvokeRequest) (*pb.InvokeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Invoke not implemented")
}

func pageTokenToOffset(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	b, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(b))
}

func offsetToPageToken(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}
