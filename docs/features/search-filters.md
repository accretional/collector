# Search Filters: Prefilter vs Postfilter

The Search feature in Collector supports two filtering strategies applied at different stages of query execution.

| Filter Type | When Applied | Purpose |
|-------------|--------------|---------|
| `filters` (Prefilter) | Before ranking | Narrow the candidate set |
| `post_filters` (Postfilter) | After ranking | Filter ranked results |

## Usage

### Go API

```go
results, err := coll.Search(ctx, &collection.SearchQuery{
    FullText: "distributed systems",

    Filters: []collection.Filter{
        {Field: "status", Operator: collection.OpEquals, Value: "active"},
    },

    PostFilters: []collection.Filter{
        {Field: "region", Operator: collection.OpEquals, Value: "US"},
    },

    Limit: 10,
})
```

### Proto/gRPC

```protobuf
message SearchRequest {
  string full_text = 3;
  repeated Filter filters = 4;       // Prefilters
  repeated Filter post_filters = 5;  // Postfilters
}
```

## Trade-offs

### Prefilter

**Advantages:**
- **Guaranteed accuracy**: All returned results strictly match the filter. No relevant items missed.
- **Smaller search space**: If filters are highly selective, search operates on fewer candidates.

**Disadvantages:**
- **Filtering bottleneck**: Identifying matching records can be slower than the search itself, especially with complex filters.
- **Index efficiency impact**: (Once we have vector indexing in place) ANN indexes are optimized for the full dataset. They don't appreciate searching arbitrary subsets, potentially degrading to less efficient paths or full scans.

### Postfilter

**Advantages:**
- **Faster ANN search**: Operates on the full, optimized index structure without subset constraints.
- **Simpler implementation**: Search and filter are separate sequential steps.

**Disadvantages:**
- **Recall issues**: Initial search retrieves K' candidates, but true nearest neighbors matching the filter might fall outside this set and be lost.
- **Wasted computation**: Many retrieved candidates may be discarded after filtering.

## Choosing the Right Filter Strategy

### Prefilter: Hard Constraints

Use prefilter when records must satisfy the condition. Non-matching records are completely excluded.

- Tenant isolation: `tenant_id = "acme"`
- Access control: `visibility = "public"`
- Data partitioning: `region = "EU"`

### Postfilter: Soft Constraints

Use postfilter when you want global ranking preserved but only show matching results. All records are ranked first, then filtered.

- Availability: `in_stock = true`
- Recency: `updated_at > last_week`
- Feature flags: `beta_enabled = true`

### Filter Selectivity

| Selectivity | Recommendation |
|-------------|----------------|
| High (>50% filtered out) | Use prefilter |
| Low (<20% filtered out) | Use postfilter |

## Supported Operators

| Operator | Description |
|----------|-------------|
| `=` | Equals |
| `!=` | Not equals |
| `>`, `>=`, `<`, `<=` | Comparison |
| `CONTAINS` | String contains |
| `IN` | Value in list |
| `EXISTS` | Field exists |
| `NOT_EXISTS` | Field missing |

## Implementation

- **Source**: `pkg/collection/search.go`
- **SQL Builder**: `pkg/db/sqlite/store.go`
- **Tests**: `pkg/collection/search_test.go`
