# Search Filters: Prefilter vs Postfilter

Collector supports two filter types: `filters` (prefilter) and `post_filters` (postfilter).

## Usage

```go
results, err := coll.Search(ctx, &collection.SearchQuery{
    FullText: "distributed systems",

    // Pre-filtering
    Filters: []collection.Filter{
        {Field: "status", Operator: collection.OpEquals, Value: "active"},
    },

    // Post-filtering
    PostFilters: []collection.Filter{
        {Field: "region", Operator: collection.OpEquals, Value: "US"},
    },

    Limit: 10,
})
```

## How Collector Implements Filtering

Both filter types are applied during SQL Search query construction (`pkg/db/sqlite/store.go`).

**Prefilters** (`filters`) are applied in `WHERE` clause before rankings are generated. For full-text search, this filters rows before BM25 scoring. For vector search, the filters are applied before KNN (so vector index operates on filtered subset).

**Postfilters** (`post_filters`) are applied once the rankings are generated via CTE wrapper.

## Tradeoffs in Collector

- **Vector search index efficiency**: sqlite-vec indices are optimized for full-dataset search. Prefilters force KNN to work on filtered subsets, which will disrupt optimization and slow down the performance. It's **preferred to use postfilters**, though we might risk missing true nearest neighbors if they fall outside initial k candidates.

- **JSON extraction cost**: Filtering uses `json_extract()` which essentially does table scans. Prefiltering runs on all records while **postfiltering runs only on top-k results**, making it more cost effective.

- **Recall vs Performance**:
    - Prefilters have much higher recall (guarantee all matching records are found) but compromises on performance (slower vector search and higher JSON overhead).
    - Postfilters optimize performance but may miss true nearest neighbors outside initial k candidates.

## When to Use The Filters

### Use Prefilters for hard constraints

- **`labels.namespace`**: Enforce tenant boundaries, as vector similarity across tenants is meaningless.
- **`labels.type`**: Filter by proto message type in polymorphic collections (comparing vectors of different types produces garbage results).
- **`created_at`/`updated_at`**: For time bounded queries where records outside the window should not be considered.

### Use Postfilters for soft constraints

- **`confidence_score`**: Apply numeric thresholds after the ranking is computed.
- **`labels.category`/`labels.source`**: Let vector search run on full dataset, then narrow results for most of the filters.
