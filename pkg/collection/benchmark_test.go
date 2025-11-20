package collection_test

import (
	"context"
	"fmt"
	"testing"

	pb "github.com/accretional/collector/gen/collector"
	// "github.com/accretional/collector/pkg/collection"
)

func BenchmarkCreateRecord(b *testing.B) {
	// Note: setupTestCollection expects *testing.T, but we can pass a dummy or cast if compatible.
	// For benchmarks, we often just ignore the T helper methods or create a custom setup.
	// Here we recycle the existing helper with a dummy T for simplicity.
	coll, cleanup := setupTestCollection(&testing.T{})
	defer cleanup()

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("bench-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
		}
		if err := coll.CreateRecord(ctx, record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetRecord(b *testing.B) {
	coll, cleanup := setupTestCollection(&testing.T{})
	defer cleanup()
	ctx := context.Background()

	// Setup: create records
	for i := 0; i < 1000; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("bench-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
		}
		if err := coll.CreateRecord(ctx, record); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-%d", i%1000)
		if _, err := coll.GetRecord(ctx, id); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUpdateRecord(b *testing.B) {
	coll, cleanup := setupTestCollection(&testing.T{})
	defer cleanup()
	ctx := context.Background()

	// Setup
	record := &pb.CollectionRecord{
		Id:        "bench-update",
		ProtoData: []byte(`{"version": 0}`),
	}
	if err := coll.CreateRecord(ctx, record); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		record.ProtoData = []byte(fmt.Sprintf(`{"version": %d}`, i))
		if err := coll.UpdateRecord(ctx, record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListRecords(b *testing.B) {
	coll, cleanup := setupTestCollection(&testing.T{})
	defer cleanup()
	ctx := context.Background()

	// Setup: create 1000 records
	for i := 0; i < 1000; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("bench-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
		}
		if err := coll.CreateRecord(ctx, record); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := coll.ListRecords(ctx, 0, 100); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearch_FullText(b *testing.B) {
	coll, cleanup := setupTestCollection(&testing.T{})
	defer cleanup()
	ctx := context.Background()

	// Setup: create searchable records
	for i := 0; i < 1000; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("search-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"content": "searchable content number %d with various terms"}`, i)),
		}
		if err := coll.CreateRecord(ctx, record); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Note: Passing pointer to SearchQuery
		if _, err := coll.Search(ctx, &collection.SearchQuery{
			FullText: "searchable content",
			Limit:    10,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearch_JSONBFilter(b *testing.B) {
	coll, cleanup := setupTestCollection(&testing.T{})
	defer cleanup()
	ctx := context.Background()

	// Setup
	for i := 0; i < 1000; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("filter-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"score": %d, "status": "active"}`, i)),
		}
		if err := coll.CreateRecord(ctx, record); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := coll.Search(ctx, &collection.SearchQuery{
			Filters: map[string]collection.Filter{
				"status": {Operator: collection.OpEquals, Value: "active"},
				"score":  {Operator: collection.OpGreaterThan, Value: 500},
			},
			Limit: 10,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConcurrentReads(b *testing.B) {
	coll, cleanup := setupTestCollection(&testing.T{})
	defer cleanup()
	ctx := context.Background()

	// Setup
	for i := 0; i < 100; i++ {
		record := &pb.CollectionRecord{
			Id:        fmt.Sprintf("concurrent-%d", i),
			ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
		}
		if err := coll.CreateRecord(ctx, record); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			id := fmt.Sprintf("concurrent-%d", i%100)
			if _, err := coll.GetRecord(ctx, id); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}
