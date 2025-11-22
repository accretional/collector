package collection_test

import (
	"context"
	"fmt"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/collection"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// Re-export for cleaner test code
var NewCollectionServer = collection.NewCollectionServer

// TestCollectionServer_Create tests the Create RPC
func TestCollectionServer_Create(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// First create a collection in the repo
	coll := &pb.Collection{
		Namespace: "test",
		Name:      "items",
	}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Test creating a record
	item := &anypb.Any{
		TypeUrl: "type.googleapis.com/test.Item",
		Value:   []byte(`{"name": "test item"}`),
	}

	req := &pb.CreateRequest{
		Namespace:      "test",
		CollectionName: "items",
		Item:           item,
		Id:             "item-1",
	}

	resp, err := server.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if resp.Id != "item-1" {
		t.Errorf("expected id 'item-1', got '%s'", resp.Id)
	}
}

func TestCollectionServer_Create_GeneratesID(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Create collection
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Create without ID
	req := &pb.CreateRequest{
		Namespace:      "test",
		CollectionName: "items",
		Item:           &anypb.Any{TypeUrl: "test", Value: []byte(`{}`)},
	}

	resp, err := server.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if resp.Id == "" {
		t.Error("expected generated ID, got empty string")
	}
}

func TestCollectionServer_Create_CollectionNotFound(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	req := &pb.CreateRequest{
		Namespace:      "nonexistent",
		CollectionName: "items",
		Item:           &anypb.Any{TypeUrl: "test", Value: []byte(`{}`)},
	}

	_, err := server.Create(ctx, req)
	if err == nil {
		t.Fatal("expected error for non-existent collection")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("expected gRPC status error")
	}

	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound code, got %v", st.Code())
	}
}

// TestCollectionServer_Get tests the Get RPC
func TestCollectionServer_Get(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Setup
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Create a record
	createReq := &pb.CreateRequest{
		Namespace:      "test",
		CollectionName: "items",
		Item:           &anypb.Any{TypeUrl: "test.Item", Value: []byte(`{"data": "value"}`)},
		Id:             "get-test",
	}
	_, err = server.Create(ctx, createReq)
	if err != nil {
		t.Fatalf("failed to create record: %v", err)
	}

	// Get the record
	getReq := &pb.GetRequest{
		Namespace:      "test",
		CollectionName: "items",
		Id:             "get-test",
	}

	resp, err := server.Get(ctx, getReq)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if resp.Item == nil {
		t.Fatal("expected item in response")
	}

	if string(resp.Item.Value) != `{"data": "value"}` {
		t.Errorf("unexpected item value: %s", string(resp.Item.Value))
	}
}

func TestCollectionServer_Get_NotFound(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Create collection but no records
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	req := &pb.GetRequest{
		Namespace:      "test",
		CollectionName: "items",
		Id:             "nonexistent",
	}

	_, err = server.Get(ctx, req)
	if err == nil {
		t.Fatal("expected error for non-existent record")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("expected gRPC status error")
	}

	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound code, got %v", st.Code())
	}
}

// TestCollectionServer_Update tests the Update RPC
func TestCollectionServer_Update(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Setup
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Create initial record
	createReq := &pb.CreateRequest{
		Namespace:      "test",
		CollectionName: "items",
		Item:           &anypb.Any{TypeUrl: "test.Item", Value: []byte(`{"version": 1}`)},
		Id:             "update-test",
	}
	_, err = server.Create(ctx, createReq)
	if err != nil {
		t.Fatalf("failed to create record: %v", err)
	}

	// Update the record
	updateReq := &pb.UpdateRequest{
		Namespace:      "test",
		CollectionName: "items",
		Id:             "update-test",
		Item:           &anypb.Any{TypeUrl: "test.Item", Value: []byte(`{"version": 2}`)},
	}

	_, err = server.Update(ctx, updateReq)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify update
	getReq := &pb.GetRequest{
		Namespace:      "test",
		CollectionName: "items",
		Id:             "update-test",
	}

	resp, err := server.Get(ctx, getReq)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(resp.Item.Value) != `{"version": 2}` {
		t.Errorf("expected updated value, got: %s", string(resp.Item.Value))
	}
}

// TestCollectionServer_Delete tests the Delete RPC
func TestCollectionServer_Delete(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Setup
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Create record
	createReq := &pb.CreateRequest{
		Namespace:      "test",
		CollectionName: "items",
		Item:           &anypb.Any{TypeUrl: "test.Item", Value: []byte(`{}`)},
		Id:             "delete-test",
	}
	_, err = server.Create(ctx, createReq)
	if err != nil {
		t.Fatalf("failed to create record: %v", err)
	}

	// Delete the record
	deleteReq := &pb.DeleteRequest{
		Namespace:      "test",
		CollectionName: "items",
		Id:             "delete-test",
	}

	_, err = server.Delete(ctx, deleteReq)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deletion
	getReq := &pb.GetRequest{
		Namespace:      "test",
		CollectionName: "items",
		Id:             "delete-test",
	}

	_, err = server.Get(ctx, getReq)
	if err == nil {
		t.Fatal("expected error for deleted record")
	}
}

// TestCollectionServer_List tests the List RPC
func TestCollectionServer_List(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Setup
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Create multiple records
	for i := 1; i <= 5; i++ {
		createReq := &pb.CreateRequest{
			Namespace:      "test",
			CollectionName: "items",
			Item:           &anypb.Any{TypeUrl: "test.Item", Value: []byte(`{}`)},
			Id:             fmt.Sprintf("list-test-%d", i),
		}
		_, err = server.Create(ctx, createReq)
		if err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// List all records
	listReq := &pb.ListRequest{
		Namespace:      "test",
		CollectionName: "items",
		PageSize:       10,
	}

	resp, err := server.List(ctx, listReq)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(resp.Items) != 5 {
		t.Errorf("expected 5 items, got %d", len(resp.Items))
	}
}

func TestCollectionServer_List_Pagination(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Setup
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Create 10 records
	for i := 1; i <= 10; i++ {
		createReq := &pb.CreateRequest{
			Namespace:      "test",
			CollectionName: "items",
			Item:           &anypb.Any{TypeUrl: "test.Item", Value: []byte(`{}`)},
			Id:             string(rune('a' + i - 1)),
		}
		_, err = server.Create(ctx, createReq)
		if err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Get first page
	listReq := &pb.ListRequest{
		Namespace:      "test",
		CollectionName: "items",
		PageSize:       3,
	}

	page1, err := server.List(ctx, listReq)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(page1.Items) != 3 {
		t.Errorf("expected 3 items in page 1, got %d", len(page1.Items))
	}

	if page1.NextPageToken == "" {
		t.Error("expected next page token")
	}

	// Get second page
	listReq.PageToken = page1.NextPageToken
	page2, err := server.List(ctx, listReq)
	if err != nil {
		t.Fatalf("List page 2 failed: %v", err)
	}

	if len(page2.Items) != 3 {
		t.Errorf("expected 3 items in page 2, got %d", len(page2.Items))
	}
}

// TestCollectionServer_Search tests the Search RPC
func TestCollectionServer_Search(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Setup
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Create searchable records
	items := []string{
		`{"title": "Go Programming", "year": 2023}`,
		`{"title": "Python Guide", "year": 2022}`,
		`{"title": "Advanced Go", "year": 2024}`,
	}

	for i, item := range items {
		createReq := &pb.CreateRequest{
			Namespace:      "test",
			CollectionName: "items",
			Item:           &anypb.Any{TypeUrl: "test.Item", Value: []byte(item)},
			Id:             string(rune('a' + i)),
		}
		_, err = server.Create(ctx, createReq)
		if err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Search for "Go"
	searchReq := &pb.SearchRequest{
		Namespace:      "test",
		CollectionName: "items",
		FullText:       "Go",
		Limit:          10,
	}

	resp, err := server.Search(ctx, searchReq)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(resp.Results) < 2 {
		t.Errorf("expected at least 2 results for 'Go', got %d", len(resp.Results))
	}
}

func TestCollectionServer_Search_WithFilters(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Setup
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Create records with different years
	items := []struct {
		id   string
		data string
	}{
		{"1", `{"title": "Book 1", "year": 2020}`},
		{"2", `{"title": "Book 2", "year": 2023}`},
		{"3", `{"title": "Book 3", "year": 2024}`},
	}

	for _, item := range items {
		createReq := &pb.CreateRequest{
			Namespace:      "test",
			CollectionName: "items",
			Item:           &anypb.Any{TypeUrl: "test.Item", Value: []byte(item.data)},
			Id:             item.id,
		}
		_, err = server.Create(ctx, createReq)
		if err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Search with filter: year >= 2023
	searchReq := &pb.SearchRequest{
		Namespace:      "test",
		CollectionName: "items",
		Filters: map[string]*pb.Filter{
			"year": {
				Operator: pb.FilterOperator_OP_GREATER_EQUAL,
				Value:    structpb.NewNumberValue(2023),
			},
		},
		Limit: 10,
	}

	resp, err := server.Search(ctx, searchReq)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results for year >= 2023, got %d", len(resp.Results))
	}
}

// TestCollectionServer_Batch tests the Batch RPC
func TestCollectionServer_Batch(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Setup
	coll := &pb.Collection{Namespace: "test", Name: "items"}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Batch operations: create 2, update 1, delete 1
	batchReq := &pb.BatchRequest{
		Namespace:      "test",
		CollectionName: "items",
		Operations: []*pb.RequestOp{
			{
				Operation: &pb.RequestOp_Create{
					Create: &pb.CreateRequest{
						Namespace:      "test",
						CollectionName: "items",
						Item:           &anypb.Any{TypeUrl: "test.Item", Value: []byte(`{"id": 1}`)},
						Id:             "batch-1",
					},
				},
			},
			{
				Operation: &pb.RequestOp_Create{
					Create: &pb.CreateRequest{
						Namespace:      "test",
						CollectionName: "items",
						Item:           &anypb.Any{TypeUrl: "test.Item", Value: []byte(`{"id": 2}`)},
						Id:             "batch-2",
					},
				},
			},
		},
	}

	resp, err := server.Batch(ctx, batchReq)
	if err != nil {
		t.Fatalf("Batch failed: %v", err)
	}

	if len(resp.Responses) != 2 {
		t.Errorf("expected 2 responses, got %d", len(resp.Responses))
	}

	// Verify both creates succeeded
	for i, r := range resp.Responses {
		if r.Status.Code != pb.Status_OK {
			t.Errorf("operation %d failed: %s", i, r.Status.Message)
		}
	}
}

// TestCollectionServer_Describe tests the Describe RPC
func TestCollectionServer_Describe(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Setup
	coll := &pb.Collection{
		Namespace:     "test",
		Name:          "items",
		IndexedFields: []string{"field1", "field2"},
	}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Create a few records
	for i := 1; i <= 3; i++ {
		createReq := &pb.CreateRequest{
			Namespace:      "test",
			CollectionName: "items",
			Item:           &anypb.Any{TypeUrl: "test.Item", Value: []byte(`{}`)},
			Id:             fmt.Sprintf("describe-test-%d", i),
		}
		_, err = server.Create(ctx, createReq)
		if err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
	}

	// Describe the collection
	descReq := &pb.DescribeRequest{
		Namespace:      "test",
		CollectionName: "items",
	}

	resp, err := server.Describe(ctx, descReq)
	if err != nil {
		t.Fatalf("Describe failed: %v", err)
	}

	if resp.RecordCount != 3 {
		t.Errorf("expected 3 records, got %d", resp.RecordCount)
	}

	if resp.CollectionDefinition.Name != "items" {
		t.Errorf("expected collection name 'items', got '%s'", resp.CollectionDefinition.Name)
	}
}

// TestCollectionServer_Modify tests the Modify RPC
func TestCollectionServer_Modify(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	// Setup
	coll := &pb.Collection{
		Namespace:     "test",
		Name:          "items",
		IndexedFields: []string{"field1"},
	}
	_, err := repo.CreateCollection(ctx, coll)
	if err != nil {
		t.Fatalf("failed to create collection: %v", err)
	}

	// Modify indexed fields
	modifyReq := &pb.ModifyRequest{
		Namespace:      "test",
		CollectionName: "items",
		IndexedFields:  []string{"field1", "field2", "field3"},
	}

	_, err = server.Modify(ctx, modifyReq)
	if err != nil {
		t.Fatalf("Modify failed: %v", err)
	}

	// Verify the modification
	descReq := &pb.DescribeRequest{
		Namespace:      "test",
		CollectionName: "items",
	}

	resp, err := server.Describe(ctx, descReq)
	if err != nil {
		t.Fatalf("Describe failed: %v", err)
	}

	if len(resp.CollectionDefinition.IndexedFields) != 3 {
		t.Errorf("expected 3 indexed fields, got %d", len(resp.CollectionDefinition.IndexedFields))
	}
}

// TestCollectionServer_Meta tests the Meta RPC
func TestCollectionServer_Meta(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	req := &pb.MetaRequest{}
	resp, err := server.Meta(ctx, req)
	if err != nil {
		t.Fatalf("Meta failed: %v", err)
	}

	if resp.ServerVersion == "" {
		t.Error("expected server version to be set")
	}
}

// TestCollectionServer_Invoke tests the Invoke RPC (should return Unimplemented)
func TestCollectionServer_Invoke(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()
	server := collection.NewCollectionServer(repo)
	ctx := context.Background()

	req := &pb.InvokeRequest{
		Namespace:      "test",
		CollectionName: "items",
		MethodName:     "CustomMethod",
	}

	_, err := server.Invoke(ctx, req)
	if err == nil {
		t.Fatal("expected Unimplemented error")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("expected gRPC status error")
	}

	if st.Code() != codes.Unimplemented {
		t.Errorf("expected Unimplemented code, got %v", st.Code())
	}
}
