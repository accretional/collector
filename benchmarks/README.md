# Benchmarks

This directory contains all benchmark-related code and documentation for comparing SQLite driver implementations.

## Structure

```
benchmarks/
├── README.md                    # This file
├── BENCHMARK.md                 # Detailed benchmark guide
├── benchmark.sh                 # Main benchmark script
├── results/                     # Benchmark results (generated)
└── pkg/db/sqlite/               # Benchmark test code
    └── benchmark_test.go        # Benchmark tests
```

## Quick Start

Run benchmarks from the project root:

```bash
./benchmarks/benchmark.sh
```

Or from this directory:

```bash
cd benchmarks
./benchmark.sh
```

## What's Benchmarked

The suite compares three SQLite driver scenarios:

1. **Fully CGo Driver** (`mattn/go-sqlite3`) - All operations through CGo
2. **Hybrid Model (CGo Enabled)** - Pure Go for regular ops, CGo for extensions
3. **Hybrid Model (CGo Disabled)** - Pure Go only

## Operations Tested

- **CRUD**: Create, Get, Update, Delete, List
- **Search**: FullText, JSONB filtering, Combined queries
- **Concurrency**: Parallel reads and writes

## Results

Results are saved in `benchmarks/results/` with timestamps.

See [BENCHMARK.md](./BENCHMARK.md) for detailed documentation.

