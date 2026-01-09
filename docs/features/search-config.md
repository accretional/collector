# Search Configuration and Behavior

This document describes the search configuration system in Collector, including defaults, validation, and behavior.

## Overview

Collector supports three types of search capabilities:

1. **Full-Text Search (FTS)** - SQLite FTS5-based text search
2. **JSON Search** - JSON field extraction and filtering
3. **Vector Search** - Semantic similarity search using embeddings

These can be enabled independently, in combination, or all disabled. Search features are optional - collections can operate without any search capabilities.

## SearchConfig

The `SearchConfig` message controls which search capabilities are enabled:

```protobuf
message SearchConfig {
  bool enable_fts = 1;
  bool enable_json = 2;
  bool enable_vector = 3;
  int32 vector_dimensions = 4;
  EmbedderType embedder_type = 5;
}
```

### Fields

- **enable_fts**: Enable full-text search using SQLite FTS5. Requires building with `-tags sqlite_fts5`.
- **enable_json**: Enable JSON column for field extraction and filtering. Required for Filters and LabelFilters in search queries.
- **enable_vector**: Enable vector/semantic search. Requires `vector_dimensions` to be set.
- **vector_dimensions**: Dimension of the embedding vectors. Must be > 0 when `enable_vector` is true. Defaults to 384 if not specified.
- **embedder_type**: Type of embedder to use. Currently supports `EMBEDDER_DETERMINISTIC` (default).

## Defaults

### When SearchConfig is Not Provided

If no `SearchConfig` is provided (or `CollectionConfig` is nil), the following defaults are applied:

- `enable_fts`: `true`
- `enable_json`: `true`
- `enable_vector`: `false`
- `vector_dimensions`: `0`
- `embedder_type`: `EMBEDDER_DETERMINISTIC`

### When SearchConfig is Partially Provided

If a `SearchConfig` is provided but some fields are unset (proto3 defaults):

- Unset boolean fields default to `false` in proto3
- **All search methods can be disabled** - this is allowed and valid
- If `enable_vector=true` but `vector_dimensions` is 0 or unset, it defaults to **384**
- If `embedder_type` is unspecified, it defaults to `EMBEDDER_DETERMINISTIC`

## Normalization

Before a `SearchConfig` is used, it is normalized by `NormalizeSearchConfig()`. This function:

1. Applies default values for missing fields (when config is nil)
2. Sets default vector dimensions (384) if vector is enabled but dimensions not specified
3. Sets default embedder type if unspecified

**Important**: Normalization happens automatically in `repo.go` and `store.go`. You should not need to call it manually, but if you do, always validate after normalization.

**Note**: All search features can be disabled. If you explicitly set all search methods to `false`, that configuration is preserved.

## Validation

`ValidateSearchConfig()` enforces the following rules:

### Vector Configuration

- If `enable_vector` is `true`:
  - `vector_dimensions` must be > 0
  - `vector_dimensions` must be <= 10000 (sanity check)

### Search Method Requirements

- **All search methods can be disabled** - this is valid and allows collections to operate without search capabilities
- If search methods are disabled, search queries will only return results based on non-search criteria (pagination, ordering, etc.)

### FTS Requirements

- If `enable_fts` is `true`, SQLite must be built with FTS5 support (`-tags sqlite_fts5`)
- FTS availability is checked at store creation time
- If FTS is requested but unavailable, store creation fails with a clear error message

### JSON Requirements

- If `enable_json` is `false`, Filters and LabelFilters in search queries will fail
- JSON is required for:
  - Field-based filtering (`SearchQuery.Filters`)
  - Label-based filtering (`SearchQuery.LabelFilters`)
  - FTS content (FTS indexes the `jsontext` column)

## Embedder Creation

Embedders are created using `embed.NewEmbedder()` which switches on the `EmbedderType` in the config, similar to how database stores are created.

### Supported Embedder Types

- **EMBEDDER_DETERMINISTIC** (default): Hash-based deterministic embedder
  - Produces stable, fixed-dimension vectors
  - Uses a fixed seed (1) for deterministic behavior
  - Suitable for testing and local development
  - Not suitable for production semantic search (use external embedders when available)

### Future Embedder Types

The `NewEmbedder()` function can be extended to support additional embedder types:
- OpenAI embeddings
- Cohere embeddings
- Local model embeddings
- Custom embedders

The pattern follows the same approach as `db.NewStore()` - a single function that switches on type and delegates to type-specific constructors.

## Search Behavior

### Query Processing

When a search query is executed:

1. **Full-Text Search**: If `FullText` is provided and FTS is enabled, uses FTS5 `MATCH` queries
2. **Vector Search**: If `SemanticText` is provided and vector search is enabled:
   - Text is embedded using the configured embedder
   - Vector similarity search is performed using SQLite vec0 extension
3. **JSON Filters**: If `Filters` or `LabelFilters` are provided:
   - Requires `enable_json` to be `true`
   - Uses `json_extract()` for field filtering
4. **Hybrid Search**: If both FTS and vector search are provided, results are combined
5. **Scalar Search**: If no `FullText` or `SemanticText` is provided, performs scalar search (no text matching, only filters/pagination/ordering)

### Fallback Behavior

- If FTS is disabled but `FullText` query is provided: **Silently falls back to scalar search** (FullText is ignored, no error)
- If vector search is disabled but `SemanticText` is provided: **Returns an error** - `"SemanticText provided but vector search is disabled (enable_vector must be true)"`
- If JSON is disabled but Filters/LabelFilters are provided: **Returns an error** - `"search with Filters or LabelFilters requires EnableJson to be true"`

### Search Result Ordering

- **Vector search**: Ordered by distance (ascending)
- **FTS search**: Ordered by BM25 score (descending)
- **Hybrid search**: Ordered by distance, then FTS score
- **Custom ordering**: If `OrderBy` is specified, uses that field with specified direction

## Configuration Examples

### Minimal Configuration (Uses Defaults)

```go
collection := &pb.Collection{
  Namespace: "demo",
  Name: "tasks",
  // No CollectionConfig - uses defaults (FTS + JSON enabled)
}
```

### FTS Only

```go
collection := &pb.Collection{
  Namespace: "demo",
  Name: "tasks",
  CollectionConfig: &pb.CollectionConfig{
    SearchConfig: &pb.SearchConfig{
      EnableFts: true,
      EnableJson: false,  // Explicitly disable JSON
      EnableVector: false,
    },
  },
}
```

### Vector Search Only

```go
collection := &pb.Collection{
  Namespace: "demo",
  Name: "tasks",
  CollectionConfig: &pb.CollectionConfig{
    SearchConfig: &pb.SearchConfig{
      EnableFts: false,
      EnableJson: false,
      EnableVector: true,
      VectorDimensions: 384,  // Explicitly set (or omit for default)
      EmbedderType: pb.EmbedderType_EMBEDDER_DETERMINISTIC,
    },
  },
}
```

### All Search Methods Enabled

```go
collection := &pb.Collection{
  Namespace: "demo",
  Name: "tasks",
  CollectionConfig: &pb.CollectionConfig{
    SearchConfig: &pb.SearchConfig{
      EnableFts: true,
      EnableJson: true,
      EnableVector: true,
      VectorDimensions: 512,
      EmbedderType: pb.EmbedderType_EMBEDDER_DETERMINISTIC,
    },
  },
}
```

### No Search Features (All Disabled)

```go
collection := &pb.Collection{
  Namespace: "demo",
  Name: "tasks",
  CollectionConfig: &pb.CollectionConfig{
    SearchConfig: &pb.SearchConfig{
      EnableFts: false,
      EnableJson: false,
      EnableVector: false,
    },
  },
}
```

This is valid - the collection will operate without search capabilities. Only basic CRUD operations and listing will be available.

### Vector with Custom Dimensions

```go
collection := &pb.Collection{
  Namespace: "demo",
  Name: "tasks",
  CollectionConfig: &pb.CollectionConfig{
    SearchConfig: &pb.SearchConfig{
      EnableVector: true,
      // vector_dimensions omitted - will default to 384
      // embedder_type omitted - will default to EMBEDDER_DETERMINISTIC
    },
  },
}
```

## Error Messages

Common validation and runtime errors and their meanings:

### Configuration Errors (at store creation)

- `"invalid search config: ..."`: Wraps validation errors from `ValidateSearchConfig()`
- `"vector_dimensions must be > 0 when enable_vector is true"`: Vector search is enabled but dimensions not set (or got 0)
- `"vector_dimensions is unreasonably large: X (max: 10000)"`: Vector dimensions exceed maximum allowed value
- `"FTS5 is not available but EnableFts is true"`: FTS requested but SQLite not built with FTS5 support
- `"failed to create embedder: ..."`: Embedder creation failed (wraps embedder-specific errors)
- `"unsupported embedder type: X"`: Unknown embedder type specified

### Search Query Errors (at search time)

- `"search with Filters or LabelFilters requires EnableJson to be true"`: JSON filtering attempted but JSON not enabled
- `"SemanticText provided but vector search is disabled (enable_vector must be true)"`: SemanticText query provided but vector search not enabled
- `"SemanticText provided but no embedder configured for vector search"`: Vector search enabled but embedder creation failed (should not happen with proper config)
- `"embedder produced X dimensions, expected Y"`: Embedder returned wrong vector dimensions (internal error)
- `"full-text search requested but FTS5 is not available"`: FTS query attempted but FTS5 not available (should be caught at store creation, but checked again for safety)

## Migration and Compatibility

### Existing Collections

- Collections created before this normalization system will continue to work
- When a collection is opened, its config is normalized and validated
- If validation fails, the collection cannot be opened (this indicates a configuration error)

### Changing Configuration

- **FTS**: Can be enabled/disabled, but requires database schema changes
- **JSON**: Can be enabled/disabled, but requires database schema changes
- **Vector**: Can be enabled/disabled, but requires database schema changes
- **Vector Dimensions**: Cannot be changed after collection creation (would require re-indexing all vectors)

**Recommendation**: Set the desired configuration at collection creation time.

## Implementation Details

### Normalization Flow

1. `CreateCollection()` or `GetCollection()` receives a `Collection` with optional `CollectionConfig`
2. `ValidateCollectionConfig()` is called to validate the config
3. `NormalizeSearchConfig()` is called to apply defaults
4. Normalized config is passed to `storeFactory()` which creates the store
5. Store validates the normalized config again (defense in depth)

### Store Creation

The store (`pkg/db/sqlite/store.go`) receives a normalized and validated config:

1. Config is normalized (if not already) using `NormalizeSearchConfig()`
2. Config is validated using `ValidateSearchConfig()`
3. Embedder is created using `embed.NewEmbedder()` (returns nil if vector search disabled)
4. Database schema is created based on enabled features:
   - Always: base schema
   - If JSON enabled: `jsontext` column
   - If vector enabled: `vector` column and `records_vec` virtual table
   - If FTS enabled: `records_fts` virtual table and triggers
5. FTS availability is checked if FTS is enabled (fails store creation if unavailable)

## Best Practices

1. **Always specify explicit config** for production collections
2. **Enable JSON** if you plan to use Filters or LabelFilters
3. **Set vector dimensions** explicitly (don't rely on defaults if you need specific dimensions)
4. **Test FTS availability** before deploying if FTS is required
5. **Validate config early** - use `ValidateCollectionConfig()` before creating collections
6. **Document your config** - keep track of which collections use which search methods
7. **Disable unused features** - if you don't need search, explicitly disable all search methods to reduce overhead

## Future Enhancements

Planned improvements to the search system:

- Support for external embedders (OpenAI, Cohere, etc.)
- Configurable embedder parameters (API keys, model names, etc.)
- Vector dimension migration tools
- Search method performance tuning
- Query optimization hints

