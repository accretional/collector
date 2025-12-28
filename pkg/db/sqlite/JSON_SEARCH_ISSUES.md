# JSON Search Implementation Issues

This document tracks known issues with the JSON search implementation in the SQLite store.

## Overview

The SQLite store supports JSON-based searching through:
- `jsontext` column: Stores proto_data as JSON for field filtering
- `labels` column: Stores metadata labels as JSON for label filtering
- `json_extract()` SQLite function: Used for querying JSON fields

## Issues

### Issue 1: Silent Error on JSON Schema Creation
**Status**: RESOLVED
**Severity**: High
**Location**: `store.go:55-65`

**Problem**:
```go
if opts.EnableJSON {
    if _, err := db.Exec(collection.JSONSchema); err != nil {
        // Error silently ignored!
    }
}
```

If the `jsontext` column fails to be added (permissions, disk space, corruption), the error is silently ignored. Later search queries will fail with cryptic "no such column: jsontext" errors.

**Impact**:
- Hard to debug when column creation fails
- Application continues in broken state

**Fix**: Only ignore "duplicate column" errors (idempotent). Return all other errors.

**Resolution**:
```go
if opts.EnableJSON {
    if _, err := db.Exec(collection.JSONSchema); err != nil {
        // Only ignore "duplicate column" errors (idempotent schema application)
        if !strings.Contains(err.Error(), "duplicate column") {
            db.Close()
            return nil, fmt.Errorf("json schema failed: %w", err)
        }
    }
}
```

**Tests**: `json_issues_test.go:TestIssue1_JSONSchemaErrorHandling`

---

### Issue 2: No EnableJSON Validation Before Search()
**Status**: RESOLVED
**Severity**: High
**Location**: `store.go:266-271`

**Problem**:
The `Search()` method unconditionally uses `json_extract()` on `r.jsontext` and `r.labels` columns without checking if `EnableJSON` was set when creating the store.

```go
// Search() assumes jsontext column exists
whereClauses = append(whereClauses, `json_extract(r.jsontext, ?) ...`)
```

If a store is created with `EnableJSON: false` but search queries with filters are attempted, queries fail with SQLite errors.

**Impact**:
- Cryptic errors when searching stores without JSON enabled
- No clear error message explaining the root cause

**Fix**: Check EnableJSON option before executing JSON queries; return helpful error message.

**Resolution**:
```go
func (s *SqliteStore) Search(ctx context.Context, q *collection.SearchQuery) ([]*collection.SearchResult, error) {
    // Validate that EnableJSON was set if JSON features are being used
    requiresJSON := len(q.Filters) > 0 || len(q.LabelFilters) > 0 || q.OrderBy != ""
    if requiresJSON && !s.options.EnableJSON {
        return nil, fmt.Errorf("search with filters, label filters, or ordering requires EnableJSON option; store was created without EnableJSON")
    }
    // ...
}
```

**Tests**: `json_issues_test.go:TestIssue2_EnableJSONValidationBeforeSearch`

---

### Issue 3: Binary Protobuf Not Converted to JSON
**Status**: RESOLVED
**Severity**: Medium
**Location**: `store.go:123-142`

**Problem**:
The store assumed `proto_data` was already JSON, but the codebase stores binary protobuf.
When `proto_data` was not valid JSON, it silently stored `"{}"` making records unsearchable.

**Resolution**:
Added `ProtoToJSONConverter` callback to SqliteStore. The store now:
1. Uses the converter to transform binary protobuf → JSON (if converter is set)
2. Falls back to using proto_data directly if it's already valid JSON
3. Falls back to `"{}"` if no converter and proto_data is not JSON (graceful degradation)

```go
// Set converter on store
store.SetJSONConverter(collection.NewStaticJSONConverter(&pb.Collection{}))

// Or use system type converters
store.SetJSONConverter(collection.GetSystemTypeConverter("Collection"))
```

**Key files**:
- `pkg/collection/options.go`: `ProtoToJSONConverter` type, `JSONConverterType` enum, `JSONConverterFactory` type
- `pkg/collection/json_converter.go`: Converter implementations including:
  - `NewStaticJSONConverter`: For compile-time known types
  - `NewDynamicJSONConverter`: For registry-based types using dynamic protobuf
  - `SystemTypeConverters`: Map of converters for system types
  - `NewRegistryConverterFactory`: Creates a factory that looks up types from the registry
- `pkg/collection/repo.go`: `JSONConverterSetter` interface, factory wiring in `GetCollection`
- `pkg/db/sqlite/store.go`: `SetJSONConverter()` and updated CreateRecord/UpdateRecord
- `pkg/registry/registry.go`: `LookupProtoByMessageName` for type lookup

**Architecture**:
The JSON conversion pipeline works as follows:
1. **Store creation**: Stores are created with `EnableJSON: true`
2. **Converter setup**: A `ProtoToJSONConverter` is set via `SetJSONConverter()`
3. **Factory pattern**: The `DefaultCollectionRepo` uses a `JSONConverterFactory` to create converters based on collection type
4. **Registry lookup**: `NewRegistryConverterFactory` creates converters by looking up FileDescriptors from the registry
5. **Fallback behavior**: If no converter is set, the store:
   - Uses proto_data directly if it's valid JSON
   - Falls back to `"{}"` otherwise (record stored but not searchable by JSON fields)

**Tests**: `json_issues_test.go:TestIssue3_InvalidJSONHandling`

---

### Issue 4: No JSON Indexing Strategy
**Status**: Deferred (Future Enhancement)
**Severity**: Low
**Location**: `store.go` (Search method)

**Problem**:
No JSON indexes are created for frequently-searched fields. All `json_extract()` calls perform full table scans.

```sql
-- Every search does unindexed extraction
SELECT ... WHERE json_extract(r.jsontext, '$.field') = ?
```

**Analysis**:
1. **Label Filters**: Now use `json_each()` which iterates through the JSON object. For small label sets (typical case), this is efficient. Expression indexes wouldn't help `json_each()`.

2. **Field Filters**: Search paths depend on the collection's schema, which varies. We can't create static indexes for unknown paths.

3. **Options considered**:
   - Expression indexes on common paths (e.g., `$.namespace`) - requires knowing schema
   - Caller-specified index creation - adds API complexity
   - Full-text search on JSON - different approach entirely

**Impact Assessment**:
- For typical workloads with small-to-medium datasets, current performance is acceptable
- Large datasets with frequent JSON searches would benefit from custom indexes
- Users can create expression indexes manually for their specific schemas

**Recommendation**: Defer as future enhancement. Current behavior is documented:
- Label filters use `json_each()` - efficient for typical label counts
- Field filters use `json_extract()` - table scan, acceptable for moderate datasets
- Power users can create custom expression indexes:
  ```sql
  CREATE INDEX idx_namespace ON records(json_extract(jsontext, '$.namespace'));
  ```

---

### Issue 5: Inconsistent Metadata Handling Between Retrieval Methods
**Status**: RESOLVED (By Design)
**Severity**: Low
**Location**: `store.go:172-200` (GetRecord), `store.go:221-260` (ListRecords)

**Problem**:
`GetRecord()` and `ListRecords()` do NOT select or populate the `jsontext` column:

```go
// GetRecord query - no jsontext
SELECT proto_data, data_uri, created_at, updated_at, labels FROM records WHERE id = ?
```

Only `Search()` uses the `jsontext` column for filtering.

**Analysis**:
This is actually correct behavior by design:
- `proto_data` is the source of truth (binary protobuf)
- `jsontext` is a derived column for search indexing only
- The `CollectionRecord` proto message has no `jsontext` field
- When retrieving records, you get `proto_data` which contains all the data
- The `jsontext` column is purely for `json_extract()` queries in Search

**Resolution**: Documented as by design. The architecture is:
1. **Storage**: `proto_data` (binary) + `jsontext` (derived JSON for indexing)
2. **Retrieval**: Returns `proto_data` - callers unmarshal to get the data
3. **Search**: Filters on `jsontext` via `json_extract()`, returns `proto_data`

This separation keeps `proto_data` as the single source of truth while enabling efficient JSON-based searching.

---

### Issue 6: Label Keys with Special Characters Not Escaped
**Status**: RESOLVED
**Severity**: Low
**Location**: `store.go:376-383`

**Problem**:
```go
for key, value := range q.LabelFilters {
    labelPath := `$.` + key  // No escaping!
    whereClauses = append(whereClauses, `json_extract(r.labels, ?) = ?`)
}
```

Label keys containing dots or special JSON path characters were not properly escaped:
- Key `"a.b.c"` becomes path `$.a.b.c` instead of `$."a.b.c"`
- Keys with quotes couldn't be queried via json_extract at all

**Resolution**: Changed from `json_extract()` with path syntax to `json_each()` which handles all key types:
```go
for key, value := range q.LabelFilters {
    // Use json_each() to filter by label key-value pairs
    // This handles all key types including those with special characters
    whereClauses = append(whereClauses, `EXISTS (SELECT 1 FROM json_each(r.labels) WHERE key = ? AND value = ?)`)
    args = append(args, key, value)
}
```

**Tests**: `json_issues_test.go:TestIssue6_LabelKeyEscaping`

---

### Issue 7: dummyStore Created Without EnableJSON
**Status**: RESOLVED
**Severity**: Low
**Location**: `pkg/server/server.go:242`

**Problem**:
```go
dummyStore, err := sqlite.NewSqliteStore(":memory:", collection.Options{})
```

The dummyStore was created with empty Options (no EnableJSON). While not used for searches, this was inconsistent.

**Resolution**: Changed to `collection.Options{EnableJSON: true}` for consistency.

---

## Test Coverage Gaps

- No tests for label keys with special characters (dots, brackets)
- No performance tests verifying index usage

## Resolution Tracking

| Issue | Status | PR/Commit | Tests Added |
|-------|--------|-----------|-------------|
| 1. Silent schema error | RESOLVED | - | TestIssue1_JSONSchemaErrorHandling |
| 2. No EnableJSON validation | RESOLVED | - | TestIssue2_EnableJSONValidationBeforeSearch |
| 3. Binary proto not converted | RESOLVED | - | TestIssue3_InvalidJSONHandling |
| 4. No JSON indexing | Deferred | - | - |
| 5. Inconsistent metadata | RESOLVED (By Design) | - | - |
| 6. Label key escaping | RESOLVED | - | TestIssue6_LabelKeyEscaping |
| 7. dummyStore options | RESOLVED | - | - |
