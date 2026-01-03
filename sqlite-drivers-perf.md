# From Pure Go to CGo: A SQLite Driver Performance Investigation

We needed a database backend to [Collector](https://github.com/accretional/collector), our framework for distributed RPC systems, for which we chose the pure Go SQLite driver (`modernc.org/sqlite`). Fast builds, easy cross-compilation, no C toolchain dependencies, and it worked well for our initial requirements: basic CRUD, backup, and simple full-text search.

We then wanted to add vector search capabilities, and that's when we discovered our pure Go driver couldn't load `sqlite-vec`, the C extension required for vector operations( in fact, we found that the driver could not load any native C extension). We investigated a couple of approaches: switching entirely to a CGo-based driver, or building a hybrid model that uses pure Go for regular operations and CGo only when needed. The hybrid approach seemed like the best of both worlds: we'd avoid CGo overhead for the majority of operations and only use it when vector search was needed.

We built a hybrid system with two connections to the same database and comprehensive benchmarks to compare these approaches. The results surprised us. The CGo-based driver wasn't just faster for vector operations, but it was significantly faster across all operations, from simple CRUD to complex searches. In some cases, it was nearly 20x faster.

This post documents our investigation, the benchmarks we ran, and what we learned about SQLite driver performance in Go.

## What is a Collector?

[Collector](https://github.com/accretional/collector) is a framework for distributed RPC systems that has service registry, collections (an ORM-like storage system for protobuf messages), dynamic dispatch, and reflection capabilities. And so, Collector needs a database to store these protobuf messages as collections, with support for full-text search, JSON queries, and eventually vector search for semantic similarity.

## Pure Go SQLite

We were using [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite), a pure Go implementation of SQLite that requires no CGo. It has some really useful features:

- **Fast builds**: No C compilation step means faster iteration during development
- **Easy cross-compilation**: Build for any platform Go supports without platform-specific toolchains
- **Single binary deployment**: No external C library dependencies
- **Portability**: Works anywhere Go runs, without requiring a C compiler

For our CRUD operations, full-text search with FTS5, and JSON queries, the pure Go driver handled our workload without issues.

The benefits of pure Go drivers are well-documented. Projects like Litestream have migrated to pure Go drivers specifically for easier deployment and cross-compilation [1]. The Go community generally recommends pure Go solutions when possible to avoid CGo complexity [2].

## Adding Vector Search

To enable semantic search in our collection system, we needed vector similarity. This allows finding records based on semantic meaning rather than exact text matches - a pretty handy tool.

We chose [sqlite-vec](https://github.com/asg017/sqlite-vec), a native SQLite extension that provides vector search capabilities. It's lightweight (about 150KB), integrates directly with SQL queries, and supports multiple distance metrics. It is also actively developing so we are sure of getting more features sooner or later.

## The Integration Challenge

Here's where we hit our first obstacle. [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite) is a pure Go transpilation of SQLite's C source code. While this makes it portable and CGo-free, **it cannot load native C extensions**.

SQLite extensions like sqlite-vec are compiled C code that must be loaded at runtime using SQLite's extension loading API (`sqlite3_load_extension`). This requires access to the system's dynamic linker (`dlopen` on Unix, `LoadLibrary` on Windows), which pure Go code cannot access without CGo.

This is a known architectural limitation of pure Go SQLite drivers. Extension loading requires C-level APIs that pure Go implementations cannot provide. The GitHub repository for modernc.org/sqlite also documents this limitation [3]. Even the official sqlite-vec documentation suggests using a CGo-based or WASM-based SQLite driver [4].

We had a few options:

1. Switch entirely to [`mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3), a CGo-based driver that supports extensions
2. Port sqlite-vec to pure Go (massive effort with performance concerns)
3. Use a separate vector database (adds operational complexity and just inconvenient)
4. Build a hybrid model: use pure Go for regular operations, CGo only for vector search

## Researching Solutions: Why Not Just Switch to CGo?

Our initial intuition was to avoid CGo if possible. The Go community suggests that CGo has overhead and complexity. We were miainly concerned about:

- **CGo overhead**: Every operation would cross the Go-C boundary, potentially adding latency
- **Slower builds**: C compilation adds time to the build process
- **Cross-compilation complexity**: Requires C toolchain for each target platform
- **Platform dependencies**: C libraries must be available on target systems

Many discussions in the Go community, such as those on r/golang, emphasize avoiding CGo when possible [2]. Why pay CGo overhead for simple CRUD operations when 99% of our operations don't need extensions?

## The Hybrid Model

We designed a hybrid architecture that would use pure Go for regular operations and CGo only when needed for extensions. The idea was to have a fast pure Go operations for the common cases, with CGo available for vector search.

### Architecture

```
┌─────────────────────────────────────┐
│      SQLite Database File           │
│         (WAL mode)                  │
├─────────────────────────────────────┤
│                                     │
│  ┌──────────────┐  ┌─────────────┐  │
│  │ modernc.org  │  │ Custom CGo  │  │
│  │   sqlite     │  │   Driver    │  │
│  │  (pure Go)   │  │ (extensions)│  │
│  │              │  │             │  │
│  │ • CRUD       │  │ • Load      │  │
│  │ • FTS5       │  │   sqlite-vec│  │
│  │ • JSON       │  │ • vec0 ops  │  │
│  │ • Schema     │  │ • KNN search│  │
│  └──────────────┘  └─────────────┘  │
│                                     │
└─────────────────────────────────────┘
```

The implementation maintained two connections to the same database file:

- **Pure Go connection**: `modernc.org/sqlite` for all regular operations (CRUD, FTS5, JSON queries)
- **CGo connection**: Custom `sqliteext` driver that could load extensions like sqlite-vec

We could maintain multiple connections that access the same database concurrently and safely with SQLite's WAL (Write-Ahead Logging) mode [5]. We used capability detection to determine when vector search was available, checking both the configuration flag and whether the CGo connection was established.

We felt that this would give us a platform for fast regular operations without CGo overhead, with vector search available when needed.

## The Benchmark Suite: Testing Our Hypothesis

To validate our hybrid approach, we built a comprehensive benchmark suite comparing three scenarios:

1. **cgo_full**: Fully CGo driver (`mattn/go-sqlite3`) for all operations
2. **hybrid_cgo**: Hybrid model with CGo enabled (two connections)
3. **pure_go**: Hybrid model with CGo disabled (pure Go only, for comparison)

We tested a range of operations to get a complete picture:

**Write Operations:**
- **CreateRecord**: Insert operations with JSON data and metadata. Each iteration inserts a new record with a unique ID and JSON payload containing `{"id": N, "name": "Record N"}`. Records are created sequentially.
- **UpdateRecord**: Update existing records with new data. We create one record initially, then update it repeatedly, changing the JSON payload to `{"version": N}` on each iteration.
- **DeleteRecord**: Remove records from database. We pre-create records equal to the number of benchmark iterations, then delete them all sequentially during the benchmark.

**Read Operations:**
- **GetRecord**: Single record retrieval by ID. We pre-create 1,000 records, then perform lookups cycling through those IDs using modulo arithmetic to simulate random access patterns.
- **ListRecords**: Paginated listing of records. We pre-create 1,000 records, then run queries that fetch 100 records at a time with `LIMIT 100 OFFSET 0`.

**Search Operations:**
- **Search_FullText**: Full-text search using FTS5 with BM25 scoring. We pre-create 1,000 records with searchable content (`"searchable content number N with various terms and keywords"`), wait 100ms for the FTS index to build, then run queries searching for `"searchable content"` with a limit of 10 results.
- **Search_JSONBFilter**: JSON field filtering with multiple conditions. We pre-create 1,000 records with JSON fields (`{"score": N, "status": "active", "category": "test"}`), then run queries filtering for `status = "active" AND score > 500 AND category = "test"` with a limit of 10 results.
- **Search_Combined**: Full-text search combined with JSON filtering. We pre-create 1,000 records with both text and JSON fields, then run queries combining full-text search for `"Document"` with JSON filters `status = "active" AND score > 500`, returning up to 10 results.

**Concurrency:**
- **ConcurrentReads**: Parallel read operations simulating multiple clients. We pre-create 100 records, then use Go's parallel benchmarking to run concurrent `GetRecord` calls across multiple goroutines, cycling through the 100 record IDs.
- **ConcurrentWrites**: Parallel write operations testing write contention. We use Go's parallel benchmarking to create records concurrently across multiple goroutines, with each record having a unique ID based on timestamp and iteration counter.

Each benchmark used isolated databases, proper schema setup, and ran for 3 seconds with multiple iterations. We followed the best practices in Go benchmarking [6].

## The Results:

Our initial expectation was that the hybrid model would be faster for regular operations, since it uses pure Go without CGo overhead. But what we found was different.

### Write Operations Performance

| Operation | CGo Full | Hybrid CGo | Pure Go | CGo Faster By |
|-----------|----------|------------|---------|---------------|
| CreateRecord | 262,004 ns | 3,877,505 ns | 3,910,506 ns | **14.8x** |
| UpdateRecord | 143,342 ns | 2,823,256 ns | 2,829,553 ns | **19.7x** |
| DeleteRecord | 257,719 ns | 4,128,032 ns | 4,046,734 ns | **16.0x** |
| ConcurrentWrites | 252,654 ns | 3,377,166 ns | 3,442,721 ns | **13.4x** |

### Read Operations Performance

| Operation | CGo Full | Hybrid CGo | Pure Go | CGo Faster By |
|-----------|----------|------------|---------|---------------|
| GetRecord | 10,114 ns | 24,329 ns | 27,903 ns | **2.4x** |
| ListRecords | 322,268 ns | 625,991 ns | 649,883 ns | **1.9x** |
| ConcurrentReads | 8,398 ns | 23,781 ns | 23,158 ns | **2.8x** |

### Search Operations Performance

| Operation | CGo Full | Hybrid CGo | Pure Go | CGo Faster By |
|-----------|----------|------------|---------|---------------|
| Search_FullText | 666,083 ns | 1,813,595 ns | 1,788,254 ns | **2.7x** |
| Search_JSONBFilter | 15,854 ns | 379,259 ns | 389,256 ns | **23.9x** |
| Search_Combined | 542,894 ns | 1,730,168 ns | 1,729,179 ns | **3.2x** |

### Memory Allocation

| Operation | CGo Full (B/op) | Hybrid CGo (B/op) | Pure Go (B/op) |
|-----------|-----------------|-------------------|----------------|
| CreateRecord | 1,831 | 1,055 | 1,055 |
| GetRecord | 1,578 | 1,252 | 1,252 |
| UpdateRecord | 1,167 | 954 | 956 |
| DeleteRecord | 303 | 144 | 144 |
| ListRecords | 66,480 | 66,809 | 66,808 |
| Search_FullText | 5,576 | 5,776 | 5,776 |
| Search_JSONBFilter | 4,752 | 6,040 | 6,040 |
| Search_Combined | 5,784 | 6,824 | 6,824 |

### Hybrid Model Overhead

We compared `hybrid_cgo` vs `pure_go` to see if maintaining two connections was adding any latency:

| Operation | Hybrid CGo | Pure Go | Difference |
|-----------|------------|---------|------------|
| CreateRecord | 3,877,505 ns | 3,910,506 ns | 0.8% |
| GetRecord | 24,329 ns | 27,903 ns | 14.7% |
| UpdateRecord | 2,823,256 ns | 2,829,553 ns | 0.2% |
| DeleteRecord | 4,128,032 ns | 4,046,734 ns | -2.0% |

The two-connection architecture adds minimal overhead (less than 1% in most cases, well within measurement variance). This confirms that the hybrid model's architecture itself isn't the problem.

## Understanding the Results

The benchmark results show that the CGo driver was much faster across all operations. After researching this, here's what we found:

### Transpiled Code Performance

`modernc.org/sqlite` works by transpiling SQLite's C source code into Go. While this achieves portability, it comes with a performance cost. We found that transpiled Go code is "at least twice as slow in every variation" compared to native C implementations [7].

The SQLite C library is the result of decades of optimization. It's actively maintained by the SQLite team with continuous performance improvements. Transpiled code, even when well-done, cannot match the optimization level of hand-tuned C code. The automated translation process loses many of the optimizations present in the original C code.

Benchmarks from the Go package repository show that CGo-based drivers like `mattn/go-sqlite3` are consistently 1.5-5.8x faster than pure Go alternatives across various operations [8]. Our results align with these findings, showing even larger gaps in some operations.

### CGo Overhead Reality

While CGo does introduce overhead for crossing the Go-C boundary, it's really small. CGo call overhead is approximately 100-500 nanoseconds per call [9]. For database operations that take microseconds to milliseconds, this overhead is negligible.

Database operations are I/O-bound and CPU-intensive. The actual work of parsing SQL, executing queries, managing transactions, and performing I/O dominates execution time. The CGo boundary crossing cost is a tiny fraction of the total operation time.

### Build Time vs Runtime

The common wisdom that "pure Go will be faster" is true, but it refers to build time, not runtime. Pure Go compiles faster because there's no C compilation step. However, for production systems, runtime performance matters far more than build time.

Build time is a one-time cost during development. Runtime performance is an ongoing cost that affects every user, every request, every day. A performance improvement in production operations far outweighs slower build times during development.

### Native C Library Optimization

The SQLite C library benefits from:

- Decades of optimization by the SQLite team
- Platform-specific optimizations (SIMD instructions, CPU-specific code paths)
- Direct access to system APIs without translation layers
- Extensive real-world testing and tuning

CGo allows Go programs to directly leverage this highly optimized C code. Pure Go drivers, even when well-implemented, cannot match this level of optimization because they're working with transpiled code that has lost many optimizations in translation.

## The Decision: Full CGo Implementation

Given the evidence, the decision became clear. The performance gap between the drivers is significant for production systems. While the hybrid model was architecturally sound (minimal overhead from two connections), it couldn't overcome the fundamental performance limitation of the pure Go driver.

We chose to switch to full CGo using `mattn/go-sqlite3`

### What We Gave Up

- **Build time**: Slower builds due to C compilation, but this only affects development, not production
- **Cross-compilation**: Requires C toolchain setup, but this is manageable with proper CI/CD configuration
- **Portability**: C library dependencies, but SQLite is widely available on all platforms we target

### What We Gained

- **Performance**: Significant speedup across all operations
- **Features**: Full extension ecosystem support
- **Simplicity**: One driver, one connection, less complexity
- **Reliability**: Mature, well-tested driver with extensive community support

## Conclusion

This investigation started with a simple task of adding vector search to our system. But it forced us to examine SQLite driver performance more carefully than we might have otherwise. What we discovered was that our initial assumptions about pure Go drivers being faster were incorrect for runtime performance.

The evidence from our benchmarks, combined with research from the broader Go community, consistently shows that CGo-based SQLite drivers significantly outperform pure Go implementations. The transpiled code approach, while valuable for portability, cannot match the optimization level of native C libraries.

The hybrid model we designed was architecturally sound. The two-connection approach added minimal overhead. But it couldn't overcome the fundamental performance limitation of the base pure Go driver. Sometimes the simpler solution (full CGo) is actually simpler, and the complex solution (hybrid model) doesn't provide the benefits we hoped for.

For others facing similar decisions, we'd recommend measuring rather than assuming. The performance characteristics of database drivers are complex and counter-intuitive. What seems like it should be faster (pure Go, no CGo overhead) may not be in practice. The evidence, in our case and in broader community benchmarks, pointed clearly toward CGo-based drivers for production workloads.

## References

1. Litestream Migration Guide: https://litestream.io/docs/migration/
2. Is it bad to use CGO? (Reddit): https://www.reddit.com/r/golang/comments/1j7kskk/is_it_bad_to_use_cgo/
3. modernc.org/sqlite GitHub Repository: https://gitlab.com/cznic/sqlite
4. Using sqlite-vec in Go: https://alexgarcia.xyz/sqlite-vec/go.html
5. SQLite WAL Mode Documentation: https://www.sqlite.org/wal.html
6. Go Benchmarking Best Practices: https://golang.org/pkg/testing/#hdr-Benchmarks
7. DataStation Blog - SQLite in Go: https://datastation.multiprocess.io/blog/2022-05-12-sqlite-in-go-with-and-without-cgo.html
8. pkg.go.dev modernc.org/sqlite Benchmarks: https://pkg.go.dev/modernc.org/sqlite/benchmark
9. The cost and complexity of CGo: https://www.cockroachlabs.com/blog/the-cost-and-complexity-of-cgo/