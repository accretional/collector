# Performance Benchmark Guide

This document explains how to run performance benchmarks comparing different SQLite driver implementations.

## Overview

The benchmark suite compares three scenarios:

1. **Fully CGo Driver** (`mattn/go-sqlite3`) - All operations through CGo
2. **Hybrid Model (CGo Enabled)** - Our hybrid model with CGo available
3. **Hybrid Model (CGo Disabled)** - Our hybrid model with pure Go only

## Prerequisites

- Linux system (recommended for CGo support)
- Go 1.24+
- CGo toolchain (for CGo benchmarks)
- `mattn/go-sqlite3` package (installed automatically by script)

## Running Benchmarks

### Quick Start

From the project root:

```bash
./benchmarks/benchmark.sh
```

Or from the benchmarks directory:

```bash
cd benchmarks
./benchmark.sh
```

This will:
1. Install `mattn/go-sqlite3` if needed
2. Run all three benchmark scenarios
3. Generate a comparison report in `benchmarks/results/`

### Skip CGo Full Driver

If you only want to compare hybrid models:

```bash
./benchmarks/benchmark.sh skip-cgo
```

This skips the fully CGo driver benchmark (useful if CGo setup is problematic).

## Benchmark Operations

The suite tests:

### CRUD Operations
- **CreateRecord**: Insert operations
- **GetRecord**: Single record retrieval
- **UpdateRecord**: Update operations
- **DeleteRecord**: Delete operations
- **ListRecords**: Paginated listing

### Search Operations
- **Search_FullText**: Full-text search with FTS5
- **Search_JSONBFilter**: JSON field filtering
- **Search_Combined**: Full-text + JSON filtering together

### Concurrent Operations
- **ConcurrentReads**: Parallel read operations
- **ConcurrentWrites**: Parallel write operations

## Understanding Results

### Output Format

Benchmark results show:
- **ns/op**: Nanoseconds per operation
- **B/op**: Bytes allocated per operation
- **allocs/op**: Number of allocations per operation

### Example Output

```
BenchmarkCreateRecord/cgo_full-8          1000    1500000 ns/op    5000 B/op    50 allocs/op
BenchmarkCreateRecord/hybrid_cgo-8        1000    1200000 ns/op    4000 B/op    40 allocs/op
BenchmarkCreateRecord/hybrid_purego-8     1000    1100000 ns/op    3500 B/op    35 allocs/op
```

**Interpretation:**
- Lower `ns/op` = faster
- Lower `B/op` = less memory
- Lower `allocs/op` = fewer allocations

### Expected Results

Based on the hybrid model design:

1. **Hybrid (CGo Enabled)**: Should be fastest for regular operations (pure Go, no CGo overhead)
2. **Hybrid (CGo Disabled)**: Should be similar to hybrid with CGo enabled (same pure Go driver)
3. **Fully CGo**: Should be slower due to CGo overhead on every operation

## Results Location

Results are saved in `benchmarks/results/`:

- `cgo_full_TIMESTAMP.txt` - Fully CGo driver results
- `hybrid_cgo_TIMESTAMP.txt` - Hybrid with CGo enabled
- `hybrid_purego_TIMESTAMP.txt` - Hybrid with CGo disabled
- `comparison_TIMESTAMP.txt` - Side-by-side comparison

## Manual Benchmarking

You can also run benchmarks manually:

```bash
# Hybrid model with CGo enabled
BENCHMARK_SCENARIO=hybrid_cgo CGO_ENABLED=1 go test -tags=benchmark -bench=. -benchmem ./benchmarks/pkg/db/sqlite

# Hybrid model with CGo disabled
BENCHMARK_SCENARIO=hybrid_purego CGO_ENABLED=0 go test -tags=benchmark -bench=. -benchmem ./benchmarks/pkg/db/sqlite

# Fully CGo driver (requires CGo)
BENCHMARK_SCENARIO=cgo_full CGO_ENABLED=1 go test -tags=benchmark -bench=. -benchmem ./benchmarks/pkg/db/sqlite
```

## Troubleshooting

### "sql: unknown driver \"sqlite3\""

**Cause:** `mattn/go-sqlite3` not installed or CGo not enabled

**Fix:**
```bash
go get github.com/mattn/go-sqlite3
go mod tidy
```

### "CGo not available"

**Cause:** CGo disabled or C toolchain missing

**Fix:** Enable CGo and ensure C compiler is installed:
```bash
CGO_ENABLED=1 go test ...
```

### Benchmark fails to compile

**Cause:** Missing dependencies or build tags

**Fix:**
```bash
go mod tidy
go test -tags=benchmark ./pkg/db/sqlite
```

## Performance Expectations

### Regular Operations (CRUD, Search)

**Expected ranking:**
1. Hybrid (CGo Enabled) - Fastest (pure Go, no overhead)
2. Hybrid (CGo Disabled) - Same as above (same pure Go driver)
3. Fully CGo - Slower (CGo overhead on every call)

**Typical differences:**
- Hybrid: ~1-2ms per operation
- Fully CGo: ~1.5-3ms per operation
- **~20-40% faster** with hybrid model

### Memory Usage

**Expected:**
- Hybrid models: Lower allocations (pure Go optimizations)
- Fully CGo: Higher allocations (CGo memory management)

## Interpreting Results

### What to Look For

1. **Speed**: Lower `ns/op` is better
2. **Memory**: Lower `B/op` and `allocs/op` is better
3. **Consistency**: Results should be consistent across runs

### Red Flags

- Hybrid model slower than fully CGo (unexpected - investigate)
- Large variance in results (may indicate system load)
- Memory leaks (increasing allocations over time)

## Next Steps

After running benchmarks:

1. **Compare results** across scenarios
2. **Identify bottlenecks** in slower operations
3. **Validate expectations** - hybrid should be faster for regular ops
4. **Document findings** for future reference

## See Also

- [SQLite Package README](./pkg/db/sqlite/README.md) - Hybrid model architecture
- [Database Package README](./pkg/db/README.md) - Factory pattern and configuration

