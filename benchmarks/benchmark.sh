#!/bin/bash
set -e

export PATH="$PATH:$(go env GOPATH)/bin"

# 1. Fix Proto definitions
echo "🔧 Fixing proto definitions..."
for f in proto/*.proto; do
    if ! grep -q "option go_package" "$f"; then
        sed 's/package collector;/package collector;\noption go_package = "github.com\/accretional\/collector\/gen\/collector";/' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    fi
done

# 2. Setup Generation Directory
echo "📁 Setting up gen directory..."
rm -rf gen
mkdir -p gen/collector

# 3. Generate Code
echo "⚡ Generating protobufs..."
cd proto
protoc --go_out=../gen/collector --go_opt=paths=source_relative \
    --go-grpc_out=../gen/collector --go-grpc_opt=paths=source_relative \
    *.proto
cd ..

# 4. Sync dependencies
echo "📦 Syncing modules..."
# FORCE UPGRADE: Update gRPC to match the installed code generator version
go get google.golang.org/grpc@latest
go get google.golang.org/protobuf@latest
go mod tidy

# SQLite Driver Performance Benchmark Script
# 
# This script compares three SQLite driver implementations:
# 1. Fully CGo driver (mattn/go-sqlite3) - All operations through CGo
# 2. Hybrid model with CGo enabled - Pure Go for regular ops, CGo for extensions
# 3. Hybrid model with CGo disabled - Pure Go only (no CGo)
#
# Usage:
#   ./benchmark.sh          # Run all benchmarks
#   ./benchmark.sh skip-cgo # Skip fully CGo driver benchmark

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

echo "╔════════════════════════════════════════════════════════════════╗"
echo "║           SQLite Driver Performance Benchmark                  ║"
echo "╚════════════════════════════════════════════════════════════════╝"
echo ""
echo "Comparing:"
echo "  1. Fully CGo driver (mattn/go-sqlite3)"
echo "  2. Hybrid model with CGo enabled"
echo "  3. Hybrid model with CGo disabled (pure Go only)"
echo ""
echo -e "${YELLOW}Note: Run this script on Linux for best results${NC}"
echo ""

# Change to project root for go commands
cd "$PROJECT_ROOT" || exit 1

# Check if mattn/go-sqlite3 is available (only needed for CGo comparison)
if [ "$1" != "skip-cgo" ]; then
    if ! go list -m github.com/mattn/go-sqlite3 > /dev/null 2>&1; then
        echo -e "${YELLOW}⚠ Installing mattn/go-sqlite3 for CGo comparison...${NC}"
        echo -e "${YELLOW}   Note: FTS5 support requires sqlite_fts5 build tag${NC}"
        go get github.com/mattn/go-sqlite3
        go mod tidy
    fi
fi

# Get script directory and project root
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Create benchmark output directory
BENCH_DIR="$SCRIPT_DIR/results"
mkdir -p "$BENCH_DIR"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

print_section() {
    echo ""
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
}

# Run benchmarks for a specific scenario
run_benchmark() {
    local scenario=$1
    local cgo_flag=$2
    local output_file="$BENCH_DIR/${scenario}_${TIMESTAMP}.txt"
    
    echo -e "${CYAN}Running benchmarks: $scenario${NC}"
    echo -e "${CYAN}CGO_ENABLED=$cgo_flag${NC}"
    echo -e "${CYAN}BENCHMARK_SCENARIO=$scenario${NC}"
    
    # For cgo_full scenario, add sqlite_fts5 tag to enable FTS5 in mattn/go-sqlite3
    local build_tags="benchmark"
    if [ "$scenario" = "cgo_full" ]; then
        build_tags="benchmark sqlite_fts5"
        echo -e "${CYAN}Using build tags: $build_tags (FTS5 enabled)${NC}"
    fi
    
    BENCHMARK_SCENARIO="$scenario" CGO_ENABLED=$cgo_flag go test \
        -tags="$build_tags" \
        -bench=. \
        -benchmem \
        -benchtime=3s \
        -run=^$ \
        ./benchmarks/pkg/db/sqlite \
        > "$output_file" 2>&1 || true
    
    if [ -f "$output_file" ]; then
        echo -e "${GREEN}✓ Results saved to: $output_file${NC}"
        # Show summary with headers
        echo ""
        printf "%-50s %12s %15s %15s %15s\n" "Benchmark Name" "Iterations" "Time/op" "Bytes/op" "Allocs/op"
        echo "────────────────────────────────────────────────────────────────────────────────"
        grep -E "^Benchmark" "$output_file" | head -20
    else
        echo -e "${RED}✗ Benchmark failed${NC}"
    fi
}

print_section "1. Fully CGo Driver (mattn/go-sqlite3)"
echo "Testing fully CGo-based driver for baseline performance..."
echo -e "${YELLOW}Note: This requires CGo and mattn/go-sqlite3${NC}"
if [ "$1" != "skip-cgo" ]; then
    if CGO_ENABLED=1 go list -m github.com/mattn/go-sqlite3 > /dev/null 2>&1; then
        run_benchmark "cgo_full" "1"
    else
        echo -e "${YELLOW}⚠ mattn/go-sqlite3 not available, skipping CGo full driver benchmark${NC}"
        echo -e "${YELLOW}   Install with: go get github.com/mattn/go-sqlite3${NC}"
    fi
else
    echo -e "${YELLOW}Skipping CGo full driver benchmark (use without 'skip-cgo' to enable)${NC}"
fi

print_section "2. Hybrid Model (CGo Enabled)"
echo "Testing our hybrid model with CGo available..."
run_benchmark "hybrid_cgo" "1"

print_section "3. Hybrid Model (CGo Disabled)"
echo "Testing our hybrid model with CGo disabled (pure Go only)..."
run_benchmark "hybrid_purego" "0"

print_section "4. Generating Comparison Report"
echo "Creating comparison report..."

REPORT_FILE="$BENCH_DIR/comparison_${TIMESTAMP}.txt"
cat > "$REPORT_FILE" << EOF
╔════════════════════════════════════════════════════════════════╗
║        SQLite Driver Performance Comparison Report             ║
║        Generated: $(date)                                      ║
╚════════════════════════════════════════════════════════════════╝

SCENARIOS TESTED:
1. Fully CGo Driver (mattn/go-sqlite3)
2. Hybrid Model with CGo Enabled
3. Hybrid Model with CGo Disabled (Pure Go)

BENCHMARK RESULTS:
EOF

# Extract and compare results
for scenario in cgo_full hybrid_cgo hybrid_purego; do
    result_file="$BENCH_DIR/${scenario}_${TIMESTAMP}.txt"
    if [ -f "$result_file" ]; then
        echo "" >> "$REPORT_FILE"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >> "$REPORT_FILE"
        echo "$scenario" >> "$REPORT_FILE"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >> "$REPORT_FILE"
        # Add column headers
        printf "%-50s %12s %15s %15s %15s\n" "Benchmark Name" "Iterations" "Time/op" "Bytes/op" "Allocs/op" >> "$REPORT_FILE"
        echo "────────────────────────────────────────────────────────────────────────────────" >> "$REPORT_FILE"
        grep -E "^Benchmark" "$result_file" >> "$REPORT_FILE" || echo "No benchmarks found" >> "$REPORT_FILE"
    fi
done

echo "" >> "$REPORT_FILE"
echo "╔════════════════════════════════════════════════════════════════╗" >> "$REPORT_FILE"
echo "║                    END OF REPORT                               ║" >> "$REPORT_FILE"
echo "╚════════════════════════════════════════════════════════════════╝" >> "$REPORT_FILE"

echo -e "${GREEN}✓ Comparison report saved to: $REPORT_FILE${NC}"
echo ""
echo -e "${CYAN}Full results available in: $BENCH_DIR/${NC}"
echo ""
cat "$REPORT_FILE"

