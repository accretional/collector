package collection

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    pb "github.com/accretional/collector/gen/collector"
)

// Test helpers
func setupTestCollection(t *testing.T) (*Collection, func()) {
    t.Helper()

    tempDir, err := os.MkdirTemp("", "collection-test-*")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }

    opts := CollectionOptions{
        BasePath: tempDir,
    }

    proto := &pb.Collection{
        Namespace: "test",
        Name:      "search_test",
        MessageType: &pb.MessageTypeRef{
            Namespace:   "test",
            MessageName: "TestMessage",
        },
        IndexedFields: []string{"name", "email"},
    }

    coll, err := NewCollection(proto, opts)
    if err != nil {
        os.RemoveAll(tempDir)
        t.Fatalf("failed to create collection: %v", err)
    }

    cleanup := func() {
        coll.Close()
        os.RemoveAll(tempDir)
    }

    return coll, cleanup
}

func createTestRecord(t *testing.T, id string, data map[string]interface{}) *pb.CollectionRecord {
    t.Helper()

    jsonData, err := json.Marshal(data)
    if err != nil {
        t.Fatalf("failed to marshal test data: %v", err)
    }

    return &pb.CollectionRecord{
        Id:        id,
        ProtoData: jsonData,
    }
}

// Full-Text Search Tests

func TestSearch_FullTextBasic(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create test records
    records := []*pb.CollectionRecord{
        createTestRecord(t, "1", map[string]interface{}{
            "name":  "Alice Johnson",
            "email": "alice@example.com",
            "bio":   "Software engineer passionate about distributed systems",
        }),
        createTestRecord(t, "2", map[string]interface{}{
            "name":  "Bob Smith",
            "email": "bob@example.com",
            "bio":   "Product manager with expertise in cloud infrastructure",
        }),
        createTestRecord(t, "3", map[string]interface{}{
            "name":  "Charlie Brown",
            "email": "charlie@example.com",
            "bio":   "DevOps engineer specializing in distributed systems",
        }),
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    tests := []struct {
        name          string
        query         string
        expectedIDs   []string
        expectResults bool
    }{
        {
            name:          "search for 'distributed systems'",
            query:         "distributed systems",
            expectedIDs:   []string{"1", "3"},
            expectResults: true,
        },
        {
            name:          "search for 'engineer'",
            query:         "engineer",
            expectedIDs:   []string{"1", "3"},
            expectResults: true,
        },
        {
            name:          "search for 'manager'",
            query:         "manager",
            expectedIDs:   []string{"2"},
            expectResults: true,
        },
        {
            name:          "search for non-existent term",
            query:         "kubernetes",
            expectedIDs:   []string{},
            expectResults: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            results, err := coll.Search(SearchQuery{
                FullText: tt.query,
                Limit:    10,
            })

            if err != nil {
                t.Fatalf("search failed: %v", err)
            }

            if !tt.expectResults {
                if len(results) != 0 {
                    t.Errorf("expected no results, got %d", len(results))
                }
                return
            }

            if len(results) != len(tt.expectedIDs) {
                t.Errorf("expected %d results, got %d", len(tt.expectedIDs), len(results))
            }

            // Check IDs
            foundIDs := make(map[string]bool)
            for _, result := range results {
                foundIDs[result.Record.Id] = true
            }

            for _, expectedID := range tt.expectedIDs {
                if !foundIDs[expectedID] {
                    t.Errorf("expected to find record %s, but it was not in results", expectedID)
                }
            }
        })
    }
}

func TestSearch_FullTextRelevanceScoring(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create records with varying relevance
    records := []*pb.CollectionRecord{
        createTestRecord(t, "1", map[string]interface{}{
            "title": "Go Programming",
            "content": "Go is a great language for building systems. Go Go Go!",
        }),
        createTestRecord(t, "2", map[string]interface{}{
            "title": "Python Guide",
            "content": "Python is popular, but Go is faster for certain tasks.",
        }),
        createTestRecord(t, "3", map[string]interface{}{
            "title": "JavaScript Basics",
            "content": "JavaScript runs in browsers and servers.",
        }),
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    results, err := coll.Search(SearchQuery{
        FullText: "Go",
        Limit:    10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(results) < 2 {
        t.Fatalf("expected at least 2 results, got %d", len(results))
    }

    // The first result should be the one with most mentions of "Go"
    if results[0].Record.Id != "1" {
        t.Errorf("expected record 1 to have highest relevance, got %s", results[0].Record.Id)
    }

    // Scores should be ordered (BM25 returns negative scores)
    for i := 1; i < len(results); i++ {
        if results[i].Score < results[i-1].Score {
            t.Errorf("scores not properly ordered: %f should be >= %f", results[i].Score, results[i-1].Score)
        }
    }
}

// JSONB Filter Tests

func TestSearch_JSONBEquals(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    records := []*pb.CollectionRecord{
        createTestRecord(t, "1", map[string]interface{}{
            "name":   "Alice",
            "age":    30,
            "status": "active",
        }),
        createTestRecord(t, "2", map[string]interface{}{
            "name":   "Bob",
            "age":    25,
            "status": "inactive",
        }),
        createTestRecord(t, "3", map[string]interface{}{
            "name":   "Charlie",
            "age":    30,
            "status": "active",
        }),
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    tests := []struct {
        name        string
        filters     map[string]Filter
        expectedIDs []string
    }{
        {
            name: "filter by status=active",
            filters: map[string]Filter{
                "status": {Operator: OpEquals, Value: "active"},
            },
            expectedIDs: []string{"1", "3"},
        },
        {
            name: "filter by age=30",
            filters: map[string]Filter{
                "age": {Operator: OpEquals, Value: 30},
            },
            expectedIDs: []string{"1", "3"},
        },
        {
            name: "filter by name=Bob",
            filters: map[string]Filter{
                "name": {Operator: OpEquals, Value: "Bob"},
            },
            expectedIDs: []string{"2"},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            results, err := coll.Search(SearchQuery{
                Filters: tt.filters,
                Limit:   10,
            })

            if err != nil {
                t.Fatalf("search failed: %v", err)
            }

            if len(results) != len(tt.expectedIDs) {
                t.Errorf("expected %d results, got %d", len(tt.expectedIDs), len(results))
            }

            foundIDs := make(map[string]bool)
            for _, result := range results {
                foundIDs[result.Record.Id] = true
            }

            for _, expectedID := range tt.expectedIDs {
                if !foundIDs[expectedID] {
                    t.Errorf("expected to find record %s", expectedID)
                }
            }
        })
    }
}

func TestSearch_JSONBComparison(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    records := []*pb.CollectionRecord{
        createTestRecord(t, "1", map[string]interface{}{"score": 85}),
        createTestRecord(t, "2", map[string]interface{}{"score": 92}),
        createTestRecord(t, "3", map[string]interface{}{"score": 78}),
        createTestRecord(t, "4", map[string]interface{}{"score": 95}),
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    tests := []struct {
        name        string
        filters     map[string]Filter
        expectedIDs []string
    }{
        {
            name: "score > 90",
            filters: map[string]Filter{
                "score": {Operator: OpGreaterThan, Value: 90},
            },
            expectedIDs: []string{"2", "4"},
        },
        {
            name: "score >= 85",
            filters: map[string]Filter{
                "score": {Operator: OpGreaterEqual, Value: 85},
            },
            expectedIDs: []string{"1", "2", "4"},
        },
        {
            name: "score < 80",
            filters: map[string]Filter{
                "score": {Operator: OpLessThan, Value: 80},
            },
            expectedIDs: []string{"3"},
        },
        {
            name: "score <= 85",
            filters: map[string]Filter{
                "score": {Operator: OpLessEqual, Value: 85},
            },
            expectedIDs: []string{"1", "3"},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            results, err := coll.Search(SearchQuery{
                Filters: tt.filters,
                Limit:   10,
            })

            if err != nil {
                t.Fatalf("search failed: %v", err)
            }

            if len(results) != len(tt.expectedIDs) {
                t.Errorf("expected %d results, got %d", len(tt.expectedIDs), len(results))
            }

            foundIDs := make(map[string]bool)
            for _, result := range results {
                foundIDs[result.Record.Id] = true
            }

            for _, expectedID := range tt.expectedIDs {
                if !foundIDs[expectedID] {
                    t.Errorf("expected to find record %s", expectedID)
                }
            }
        })
    }
}

func TestSearch_JSONBContains(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    records := []*pb.CollectionRecord{
        createTestRecord(t, "1", map[string]interface{}{
            "email": "alice@example.com",
        }),
        createTestRecord(t, "2", map[string]interface{}{
            "email": "bob@company.com",
        }),
        createTestRecord(t, "3", map[string]interface{}{
            "email": "charlie@example.org",
        }),
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    results, err := coll.Search(SearchQuery{
        Filters: map[string]Filter{
            "email": {Operator: OpContains, Value: "example"},
        },
        Limit: 10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    expectedIDs := []string{"1", "3"}
    if len(results) != len(expectedIDs) {
        t.Errorf("expected %d results, got %d", len(expectedIDs), len(results))
    }

    foundIDs := make(map[string]bool)
    for _, result := range results {
        foundIDs[result.Record.Id] = true
    }

    for _, expectedID := range expectedIDs {
        if !foundIDs[expectedID] {
            t.Errorf("expected to find record %s", expectedID)
        }
    }
}

func TestSearch_JSONBNestedFields(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    records := []*pb.CollectionRecord{
        createTestRecord(t, "1", map[string]interface{}{
            "user": map[string]interface{}{
                "profile": map[string]interface{}{
                    "city": "San Francisco",
                },
            },
        }),
        createTestRecord(t, "2", map[string]interface{}{
            "user": map[string]interface{}{
                "profile": map[string]interface{}{
                    "city": "New York",
                },
            },
        }),
        createTestRecord(t, "3", map[string]interface{}{
            "user": map[string]interface{}{
                "profile": map[string]interface{}{
                    "city": "San Francisco",
                },
            },
        }),
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    results, err := coll.Search(SearchQuery{
        Filters: map[string]Filter{
            "user.profile.city": {Operator: OpEquals, Value: "San Francisco"},
        },
        Limit: 10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    expectedIDs := []string{"1", "3"}
    if len(results) != len(expectedIDs) {
        t.Errorf("expected %d results, got %d", len(expectedIDs), len(results))
    }

    foundIDs := make(map[string]bool)
    for _, result := range results {
        foundIDs[result.Record.Id] = true
    }

    for _, expectedID := range expectedIDs {
        if !foundIDs[expectedID] {
            t.Errorf("expected to find record %s", expectedID)
        }
    }
}

func TestSearch_JSONBExists(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    records := []*pb.CollectionRecord{
        createTestRecord(t, "1", map[string]interface{}{
            "name":  "Alice",
            "phone": "123-456-7890",
        }),
        createTestRecord(t, "2", map[string]interface{}{
            "name": "Bob",
        }),
        createTestRecord(t, "3", map[string]interface{}{
            "name":  "Charlie",
            "phone": "098-765-4321",
        }),
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    // Test EXISTS
    results, err := coll.Search(SearchQuery{
        Filters: map[string]Filter{
            "phone": {Operator: OpExists},
        },
        Limit: 10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    expectedIDs := []string{"1", "3"}
    if len(results) != len(expectedIDs) {
        t.Errorf("expected %d results, got %d", len(expectedIDs), len(results))
    }

    // Test NOT_EXISTS
    results, err = coll.Search(SearchQuery{
        Filters: map[string]Filter{
            "phone": {Operator: OpNotExists},
        },
        Limit: 10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(results) != 1 || results[0].Record.Id != "2" {
        t.Errorf("expected only record 2, got %d results", len(results))
    }
}

// Combined Search Tests

func TestSearch_CombinedFullTextAndFilters(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    records := []*pb.CollectionRecord{
        createTestRecord(t, "1", map[string]interface{}{
            "title":  "Go Programming Guide",
            "author": "Alice",
            "year":   2023,
        }),
        createTestRecord(t, "2", map[string]interface{}{
            "title":  "Advanced Go Techniques",
            "author": "Bob",
            "year":   2024,
        }),
        createTestRecord(t, "3", map[string]interface{}{
            "title":  "Python Programming",
            "author": "Alice",
            "year":   2023,
        }),
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    // Search for "Go" by author "Alice"
    results, err := coll.Search(SearchQuery{
        FullText: "Go",
        Filters: map[string]Filter{
            "author": {Operator: OpEquals, Value: "Alice"},
        },
        Limit: 10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(results) != 1 {
        t.Errorf("expected 1 result, got %d", len(results))
    }

    if len(results) > 0 && results[0].Record.Id != "1" {
        t.Errorf("expected record 1, got %s", results[0].Record.Id)
    }
}

func TestSearch_MultipleFilters(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    records := []*pb.CollectionRecord{
        createTestRecord(t, "1", map[string]interface{}{
            "status": "active",
            "score":  85,
            "city":   "SF",
        }),
        createTestRecord(t, "2", map[string]interface{}{
            "status": "active",
            "score":  92,
            "city":   "NYC",
        }),
        createTestRecord(t, "3", map[string]interface{}{
            "status": "inactive",
            "score":  88,
            "city":   "SF",
        }),
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    results, err := coll.Search(SearchQuery{
        Filters: map[string]Filter{
            "status": {Operator: OpEquals, Value: "active"},
            "score":  {Operator: OpGreaterEqual, Value: 90},
        },
        Limit: 10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(results) != 1 {
        t.Errorf("expected 1 result, got %d", len(results))
    }

    if len(results) > 0 && results[0].Record.Id != "2" {
        t.Errorf("expected record 2, got %s", results[0].Record.Id)
    }
}

// Ordering and Pagination Tests

func TestSearch_Ordering(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    records := []*pb.CollectionRecord{
        createTestRecord(t, "1", map[string]interface{}{"name": "Charlie", "age": 30}),
        createTestRecord(t, "2", map[string]interface{}{"name": "Alice", "age": 25}),
        createTestRecord(t, "3", map[string]interface{}{"name": "Bob", "age": 35}),
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    // Order by age ascending
    results, err := coll.Search(SearchQuery{
        OrderBy:   "age",
        Ascending: true,
        Limit:     10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    expectedOrder := []string{"2", "1", "3"}
    for i, expectedID := range expectedOrder {
        if results[i].Record.Id != expectedID {
            t.Errorf("position %d: expected %s, got %s", i, expectedID, results[i].Record.Id)
        }
    }

    // Order by age descending
    results, err = coll.Search(SearchQuery{
        OrderBy:   "age",
        Ascending: false,
        Limit:     10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    expectedOrder = []string{"3", "1", "2"}
    for i, expectedID := range expectedOrder {
        if results[i].Record.Id != expectedID {
            t.Errorf("position %d: expected %s, got %s", i, expectedID, results[i].Record.Id)
        }
    }
}

func TestSearch_Pagination(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create 10 records
    for i := 1; i <= 10; i++ {
        record := createTestRecord(t, fmt.Sprintf("%d", i), map[string]interface{}{
            "index": i,
        })
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    // Get first page
    page1, err := coll.Search(SearchQuery{
        Limit:  3,
        Offset: 0,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(page1) != 3 {
        t.Errorf("expected 3 results in page 1, got %d", len(page1))
    }

    // Get second page
    page2, err := coll.Search(SearchQuery{
        Limit:  3,
        Offset: 3,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(page2) != 3 {
        t.Errorf("expected 3 results in page 2, got %d", len(page2))
    }

    // Ensure no overlap
    page1IDs := make(map[string]bool)
    for _, result := range page1 {
        page1IDs[result.Record.Id] = true
    }

    for _, result := range page2 {
        if page1IDs[result.Record.Id] {
            t.Errorf("record %s appears in both pages", result.Record.Id)
        }
    }
}

// Edge Cases

func TestSearch_EmptyQuery(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create a record
    record := createTestRecord(t, "1", map[string]interface{}{"name": "test"})
    if err := coll.CreateRecord(record); err != nil {
        t.Fatalf("failed to create record: %v", err)
    }

    // Empty query should return all records
    results, err := coll.Search(SearchQuery{
        Limit: 10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(results) != 1 {
        t.Errorf("expected 1 result, got %d", len(results))
    }
}

func TestSearch_NoResults(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    results, err := coll.Search(SearchQuery{
        FullText: "nonexistent",
        Limit:    10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(results) != 0 {
        t.Errorf("expected 0 results, got %d", len(results))
    }
}

func TestSearch_UpdateMaintainsFTS(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create a record
    record := createTestRecord(t, "1", map[string]interface{}{
        "content": "original content",
    })
    if err := coll.CreateRecord(record); err != nil {
        t.Fatalf("failed to create record: %v", err)
    }

    // Search for original content
    results, err := coll.Search(SearchQuery{
        FullText: "original",
        Limit:    10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(results) != 1 {
        t.Errorf("expected 1 result for 'original', got %d", len(results))
    }

    // Update the record
    updatedRecord := createTestRecord(t, "1", map[string]interface{}{
        "content": "updated content",
    })
    if err := coll.UpdateRecord(updatedRecord); err != nil {
        t.Fatalf("failed to update record: %v", err)
    }

    // Search for updated content
    results, err = coll.Search(SearchQuery{
        FullText: "updated",
        Limit:    10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(results) != 1 {
        t.Errorf("expected 1 result for 'updated', got %d", len(results))
    }

    // Original content should not be found
    results, err = coll.Search(SearchQuery{
        FullText: "original",
        Limit:    10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(results) != 0 {
        t.Errorf("expected 0 results for 'original' after update, got %d", len(results))
    }
}

func TestSearch_DeleteRemovesFromFTS(t *testing.T) {
    coll, cleanup := setupTestCollection(t)
    defer cleanup()

    // Create records
    records := []*pb.CollectionRecord{
        createTestRecord(t, "1", map[string]interface{}{"content": "test content"}),
        createTestRecord(t, "2", map[string]interface{}{"content": "other content"}),
    }

    for _, record := range records {
        if err := coll.CreateRecord(record); err != nil {
            t.Fatalf("failed to create record: %v", err)
        }
    }

    // Delete one record
    if err := coll.DeleteRecord("1"); err != nil {
        t.Fatalf("failed to delete record: %v", err)
    }

    // Search should only find remaining record
    results, err := coll.Search(SearchQuery{
        FullText: "content",
        Limit:    10,
    })

    if err != nil {
        t.Fatalf("search failed: %v", err)
    }

    if len(results) != 1 {
        t.Errorf("expected 1 result after delete, got %d", len(results))
    }

    if len(results) > 0 && results[0].Record.Id != "2" {
        t.Errorf("expected record 2, got %s", results[0].Record.Id)
    }
}
