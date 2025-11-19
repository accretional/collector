package collection

import (
    "fmt"
    "testing"

    pb "github.com/accretional/collector/gen/collector"
)

func BenchmarkCreateRecord(b *testing.B) {
    coll, cleanup := setupTestCollection(&testing.T{})
    defer cleanup()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("bench-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
        }
        if err := coll.CreateRecord(record); err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkGetRecord(b *testing.B) {
    coll, cleanup := setupTestCollection(&testing.T{})
    defer cleanup()

    // Setup: create records
    for i := 0; i < 1000; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("bench-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
        }
        coll.CreateRecord(record)
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        id := fmt.Sprintf("bench-%d", i%1000)
        if _, err := coll.GetRecord(id); err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkUpdateRecord(b *testing.B) {
    coll, cleanup := setupTestCollection(&testing.T{})
    defer cleanup()

    // Setup
    record := &pb.CollectionRecord{
        Id:        "bench-update",
        ProtoData: []byte(`{"version": 0}`),
    }
    coll.CreateRecord(record)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        record.ProtoData = []byte(fmt.Sprintf(`{"version": %d}`, i))
        if err := coll.UpdateRecord(record); err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkListRecords(b *testing.B) {
    coll, cleanup := setupTestCollection(&testing.T{})
    defer cleanup()

    // Setup: create 1000 records
    for i := 0; i < 1000; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("bench-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
        }
        coll.CreateRecord(record)
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        if _, err := coll.ListRecords(0, 100); err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkSearch_FullText(b *testing.B) {
    coll, cleanup := setupTestCollection(&testing.T{})
    defer cleanup()

    // Setup: create searchable records
    for i := 0; i < 1000; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("search-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"content": "searchable content number %d with various terms"}`, i)),
        }
        coll.CreateRecord(record)
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        if _, err := coll.Search(SearchQuery{
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

    // Setup
    for i := 0; i < 1000; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("filter-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"score": %d, "status": "active"}`, i)),
        }
        coll.CreateRecord(record)
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        if _, err := coll.Search(SearchQuery{
            Filters: map[string]Filter{
                "status": {Operator: OpEquals, Value: "active"},
                "score":  {Operator: OpGreaterThan, Value: 500},
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

    // Setup
    for i := 0; i < 100; i++ {
        record := &pb.CollectionRecord{
            Id:        fmt.Sprintf("concurrent-%d", i),
            ProtoData: []byte(fmt.Sprintf(`{"id": %d}`, i)),
        }
        coll.CreateRecord(record)
    }

    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        i := 0
        for pb.Next() {
            id := fmt.Sprintf("concurrent-%d", i%100)
            if _, err := coll.GetRecord(id); err != nil {
                b.Fatal(err)
            }
            i++
        }
    })
}
