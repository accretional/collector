#!/bin/bash
set -e

# Get script directory and project root
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Change to project root for go commands
cd "$PROJECT_ROOT" || exit 1

# Add Go's bin directory to the PATH
export PATH="$PATH:$(go env GOPATH)/bin"

echo "🔧 Fixing proto definitions..."
for f in proto/*.proto; do
    if ! grep -q "option go_package" "$f"; then
        sed 's/package collector;/package collector;\noption go_package = "github.com\/accretional\/collector\/gen\/collector";/' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    fi
done

echo "📁 Setting up gen directory..."
rm -rf gen
mkdir -p gen/collector

echo "⚡ Generating protobufs..."
cd proto
protoc --go_out=../gen/collector --go_opt=paths=source_relative \
    --go-grpc_out=../gen/collector --go-grpc_opt=paths=source_relative \
    *.proto
cd ..

echo "📦 Syncing modules..."
go get google.golang.org/grpc@latest
go get google.golang.org/protobuf@latest
go mod tidy

# --- Benchmark script starts here ---

# SQLite Driver Performance Benchmark Script
# 
# This script runs the CGo-based driver benchmark.
#
# Usage:
#   ./benchmark.sh

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
echo "Running:"
echo "  - Fully CGo driver (mattn/go-sqlite3)"

echo ""
echo -e "${YELLOW}Note: Run this script on Linux for best results${NC}"
echo ""

# Check if mattn/go-sqlite3 is available
if ! go list -m github.com/mattn/go-sqlite3 > /dev/null 2>&1; then
    echo -e "${YELLOW}⚠ Installing mattn/go-sqlite3 for CGo comparison...${NC}"
    echo -e "${YELLOW}   Note: FTS5 support requires sqlite_fts5 build tag${NC}"
    go get github.com/mattn/go-sqlite3
    go mod tidy
fi

# Create benchmark output directory
BENCH_DIR="$PROJECT_ROOT/benchmarks/results"
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
    
    local build_tags="benchmark sqlite_fts5"
    echo -e "${CYAN}Using build tags: $build_tags (FTS5 enabled)${NC}"
    
    CGO_ENABLED=$cgo_flag go test \
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
        printf "% -50s %12s %15s %15s %15s\n" "Benchmark Name" "Iterations" "Time/op" "Bytes/op" "Allocs/op"
        echo "────────────────────────────────────────────────────────────────────────────────"
        grep -E "^Benchmark" "$output_file" | head -20
    else
        echo -e "${RED}✗ Benchmark failed${NC}"
    fi
}

print_section "Fully CGo Driver (mattn/go-sqlite3)"
echo "Testing fully CGo-based driver performance..."

if CGO_ENABLED=1 go list -m github.com/mattn/go-sqlite3 > /dev/null 2>&1; then
    run_benchmark "cgo_full" "1"
else
    echo -e "${YELLOW}⚠ mattn/go-sqlite3 not available, skipping CGo full driver benchmark${NC}"
    echo -e "${YELLOW}   Install with: go get github.com/mattn/go-sqlite3${NC}"
fi

echo ""
echo -e "${GREEN}✓ Benchmark complete.${NC}"
echo -e "${CYAN}Full results available in: $BENCH_DIR/${NC}"
echo ""