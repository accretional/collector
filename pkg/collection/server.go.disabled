package collection

import (
	"context"
	"strconv"

	"github.com/accretional/collector/gen/collector"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type Server struct {
	collector.UnimplementedCollectionServerServer
	collection *Collection
}

func NewServer(collection *Collection) *Server {
	return &Server{
		collection: collection,
	}
}

func (s *Server) Create(ctx context.Context, req *collector.CreateRequest) (*collector.CreateResponse, error) {
	data, err := proto.Marshal(req.Item)
	if err != nil {
		return nil, err
	}

	err = s.collection.CreateRecord(ctx, &collector.CollectionRecord{
		Id:        req.Id,
		ProtoData: data,
	})
	if err != nil {
		return nil, err
	}

	return &collector.CreateResponse{Id: req.Id}, nil
}

func (s *Server) Get(ctx context.Context, req *collector.GetRequest) (*collector.GetResponse, error) {
	record, err := s.collection.GetRecord(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	// Unmarshal the data into a generic anypb.Any
	any := &anypb.Any{}
	err = proto.Unmarshal(record.ProtoData, any)
	if err != nil {
		return nil, err
	}

	return &collector.GetResponse{Item: any}, nil
}

func (s *Server) Update(ctx context.Context, req *collector.UpdateRequest) (*collector.UpdateResponse, error) {
	data, err := proto.Marshal(req.Item)
	if err != nil {
		return nil, err
	}

	err = s.collection.UpdateRecord(ctx, &collector.CollectionRecord{
		Id:        req.Id,
		ProtoData: data,
	})
	if err != nil {
		return nil, err
	}

	return &collector.UpdateResponse{}, nil
}

func (s *Server) Delete(ctx context.Context, req *collector.DeleteRequest) (*collector.DeleteResponse, error) {
	err := s.collection.DeleteRecord(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &collector.DeleteResponse{}, nil
}

func (s *Server) List(ctx context.Context, req *collector.ListRequest) (*collector.ListResponse, error) {
	page := 1
	if req.PageToken != "" {
		p, err := strconv.Atoi(req.PageToken)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid page token")
		}
		page = p
	}
	offset := int(req.PageSize) * (page - 1)
	records, err := s.collection.ListRecords(ctx, offset, int(req.PageSize))
	if err != nil {
		return nil, err
	}

	items := make([]*anypb.Any, len(records))
	for i, record := range records {
		any := &anypb.Any{}
		err = proto.Unmarshal(record.ProtoData, any)
		if err != nil {
			return nil, err
		}
		items[i] = any
	}

	var nextPageToken string
	if len(records) == int(req.PageSize) {
		nextPageToken = strconv.Itoa(page + 1)
	}

	return &collector.ListResponse{Items: items, NextPageToken: nextPageToken}, nil
}

func (s *Server) Search(ctx context.Context, req *collector.SearchRequest) (*collector.SearchResponse, error) {
	filters := make(map[string]Filter)
	if req.Query != nil {
		for k, v := range req.Query.AsMap() {
			filters[k] = Filter{
				Operator: OpEquals,
				Value:    v,
			}
		}
	}

	query := &SearchQuery{
		Filters: filters,
		Limit:   int(req.Limit),
		Offset:  int(req.Offset),
		OrderBy: req.OrderBy,
	}

	results, err := s.collection.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	items := make([]*anypb.Any, len(results))
	scores := make(map[string]float64)
	for i, result := range results {
		any := &anypb.Any{}
		err = proto.Unmarshal(result.Record.ProtoData, any)
		if err != nil {
			return nil, err
		}
		items[i] = any
		scores[result.Record.Id] = result.Score
	}

	return &collector.SearchResponse{Items: items, Scores: scores}, nil
}

func (s *Server) Invoke(ctx context.Context, req *collector.InvokeRequest) (*collector.InvokeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Invoke not implemented")
}
