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
**Status**: Open
**Severity**: Medium
**Location**: `store.go` (Search method)

**Problem**:
No JSON indexes are created for frequently-searched fields. All `json_extract()` calls perform full table scans.

```sql
-- Every search does unindexed extraction
SELECT ... WHERE json_extract(r.jsontext, '$.field') = ?
```

**Impact**:
- O(n) complexity for all JSON searches
- Poor performance on large datasets
- Registry namespace filtering scans all collections

**Fix**: Create indexes on frequently-searched JSON paths, especially `$.namespace` for labels.

---

### Issue 5: Inconsistent Metadata Handling Between Retrieval Methods
**Status**: Open
**Severity**: Medium
**Location**: `store.go:145-165` (GetRecord), `store.go:221-251` (ListRecords)

**Problem**:
`GetRecord()` and `ListRecords()` do NOT select or populate the `jsontext` column:

```go
// GetRecord query - no jsontext
SELECT proto_data, data_uri, created_at, updated_at, labels FROM records WHERE id = ?
```

Only `Search()` populates records with JSON-derived data.

**Impact**:
- Confusion about when JSON data is available
- Inconsistent record contents depending on retrieval method

**Fix**: Either include `jsontext` in all retrieval methods, or document the behavior clearly.

---

### Issue 6: Label Keys with Special Characters Not Escaped
**Status**: Open
**Severity**: Low
**Location**: `store.go:303-309`

**Problem**:
```go
for key, value := range q.LabelFilters {
    labelPath := `$.` + key  // No escaping!
    whereClauses = append(whereClauses, `json_extract(r.labels, ?) = ?`)
}
```

Label keys containing dots or special JSON path characters are not properly escaped:
- Key `"a.b.c"` becomes path `$.a.b.c` instead of `$."a.b.c"`
- This silently returns wrong results

**Impact**:
- Label searches fail for keys with special characters
- Silent incorrect results

**Fix**: Properly escape label keys for JSON path syntax.

---

### Issue 7: dummyStore Created Without EnableJSON
**Status**: Open
**Severity**: Low
**Location**: `pkg/server/server.go:240`

**Problem**:
```go
dummyStore, err := sqlite.NewSqliteStore(":memory:", collection.Options{})
```

The dummyStore is created with empty Options (no EnableJSON). While currently not used for searches, this is inconsistent and could cause issues if the implementation changes.

**Impact**:
- Low immediate risk (not used for searches)
- Maintenance/refactoring risk

**Fix**: Create with `collection.Options{EnableJSON: true}` for consistency.

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
| 4. No JSON indexing | Open | - | - |
| 5. Inconsistent metadata | Open | - | - |
| 6. Label key escaping | Open | - | - |
| 7. dummyStore options | Open | - | - |
