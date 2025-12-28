# JSON Search Issues - Status Summary

**Last Updated**: 2025-12-28

All issues resolved except one deferred enhancement.

| Issue | Status | Description |
|-------|--------|-------------|
| 1 | ✅ RESOLVED | Silent error on JSON schema creation - now returns errors |
| 2 | ✅ RESOLVED | No EnableJSON validation - now validates before search |
| 3 | ✅ RESOLVED | Binary proto not converted - ProtoToJSONConverter added |
| 4 | ⏸️ DEFERRED | No JSON indexing - documented as future enhancement |
| 5 | ✅ BY DESIGN | Inconsistent metadata - jsontext is search-only by design |
| 6 | ✅ RESOLVED | Label key escaping - uses json_each() for all key types |
| 7 | ✅ RESOLVED | dummyStore options - now uses EnableJSON: true |

---

## Deferred: Issue 4 - JSON Indexing

JSON fields use full table scans via `json_extract()`. For typical workloads this is acceptable.

**Current behavior**:
- Label filters use `json_each()` - efficient for small label sets
- Field filters use `json_extract()` - table scan

**For large datasets**, users can create expression indexes:
```sql
CREATE INDEX idx_namespace ON records(json_extract(jsontext, '$.namespace'));
```

---

## Architecture

```
proto_data (binary) ─→ ProtoToJSONConverter ─→ jsontext (search index)
                                                    ↓
                                              json_extract() queries
```

- `proto_data`: Source of truth (binary protobuf)
- `jsontext`: Derived column for search indexing only
- Retrieval returns `proto_data`, not `jsontext`
