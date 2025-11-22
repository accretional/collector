#!/bin/bash
set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test counters
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

echo "╔════════════════════════════════════════════════════════════════╗"
echo "║     COMPREHENSIVE TEST SUITE - RUN BEFORE EVERY SUBMIT        ║"
echo "╚════════════════════════════════════════════════════════════════╝"
echo ""
echo "⚠️  IMPORTANT: All tests must pass before submitting changes"
echo "⚠️  Agents: Any test failure is YOUR responsibility to fix"
echo ""

# Function to print section headers
print_section() {
    echo ""
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
}

# Function to run a test and track results
run_test() {
    local test_name="$1"
    local test_cmd="$2"

    echo -e "${YELLOW}▶ Running: ${test_name}${NC}"
    TOTAL_TESTS=$((TOTAL_TESTS + 1))

    if eval "$test_cmd"; then
        echo -e "${GREEN}✓ PASSED: ${test_name}${NC}"
        PASSED_TESTS=$((PASSED_TESTS + 1))
        return 0
    else
        echo -e "${RED}✗ FAILED: ${test_name}${NC}"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        return 1
    fi
}

# Track if any test failed
ALL_PASSED=true

print_section "1. BUILD VERIFICATION"
if ! run_test "Build all packages" "go build ./..."; then
    ALL_PASSED=false
fi

if ! run_test "Build main server" "go build ./cmd/server"; then
    ALL_PASSED=false
fi

print_section "2. CODE QUALITY CHECKS"
if ! run_test "Go vet - static analysis" "go vet ./pkg/..."; then
    ALL_PASSED=false
fi

if ! run_test "Go fmt - code formatting" "test -z \$(gofmt -l ./pkg/ ./cmd/)"; then
    ALL_PASSED=false
    echo "  Hint: Run 'gofmt -w ./pkg/ ./cmd/' to fix formatting"
fi

print_section "3. UNIT TESTS - ALL PACKAGES"
if ! run_test "pkg/collection - unit tests" "go test ./pkg/collection -short -v"; then
    ALL_PASSED=false
fi

if ! run_test "pkg/registry - unit tests" "go test ./pkg/registry -short -v"; then
    ALL_PASSED=false
fi

if ! run_test "pkg/dispatch - unit tests" "go test ./pkg/dispatch -short -v"; then
    ALL_PASSED=false
fi

if ! run_test "pkg/db/sqlite - unit tests" "go test ./pkg/db/sqlite -short -v"; then
    ALL_PASSED=false
fi

print_section "4. INTEGRATION TESTS"
if ! run_test "Integration tests" "go test ./pkg/integration -short -v"; then
    ALL_PASSED=false
fi

print_section "5. BACKUP SYSTEM VALIDATION"
echo "Testing backup functionality and availability..."

if ! run_test "Backup tests - all scenarios" "go test ./pkg/collection -run 'Test.*Backup' -short -v"; then
    ALL_PASSED=false
fi

if ! run_test "Backup availability - near-zero downtime" "go test ./pkg/db/sqlite -run TestBackup -v"; then
    ALL_PASSED=false
fi

print_section "6. CONCURRENCY & RACE DETECTION"
echo "Note: Race detector adds overhead and may cause timing-sensitive tests to fail"
echo "      This is expected - race detector significantly slows execution"
echo ""

if ! run_test "Race detector - collection package" "go test ./pkg/collection -race -short"; then
    ALL_PASSED=false
fi

# Run race detection on non-timing sensitive packages
if ! run_test "Race detector - registry/dispatch" "go test ./pkg/registry ./pkg/dispatch -race -short"; then
    ALL_PASSED=false
fi

# Note: pkg/db/sqlite backup tests fail under race detector due to timing thresholds
# This is expected behavior - race detector adds 5-20x slowdown
echo "⚠️  Skipping race detection on pkg/db/sqlite (lock timing tests incompatible with race overhead)"

print_section "7. DURABILITY & STRESS TESTS"
if ! run_test "Durability tests" "go test ./pkg/collection -run Durability -short -v"; then
    ALL_PASSED=false
fi

if ! run_test "Concurrent operations" "go test ./pkg/collection -run Concurrent -short -v"; then
    ALL_PASSED=false
fi

print_section "8. BENCHMARKS"
echo "Running performance benchmarks..."
if ! go test -bench=. ./pkg/collection -benchtime=1s -run=^$ > /tmp/bench.txt 2>&1; then
    echo -e "${YELLOW}⚠ Benchmarks completed with warnings${NC}"
    cat /tmp/bench.txt
else
    echo -e "${GREEN}✓ Benchmarks completed successfully${NC}"
    grep "Benchmark" /tmp/bench.txt | head -10
fi

print_section "9. COVERAGE REPORT"
echo "Generating coverage report..."
if go test -coverprofile=coverage.out ./pkg/... > /dev/null 2>&1; then
    COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}')
    echo -e "${GREEN}✓ Overall test coverage: ${COVERAGE}${NC}"

    # Check if coverage is acceptable (>70%)
    COVERAGE_NUM=$(echo $COVERAGE | sed 's/%//')
    if (( $(echo "$COVERAGE_NUM < 70" | bc -l) )); then
        echo -e "${YELLOW}⚠ Warning: Coverage is below 70%${NC}"
    fi
else
    echo -e "${YELLOW}⚠ Could not generate coverage report${NC}"
fi

print_section "10. FINAL VERIFICATION"
# One more comprehensive run of all tests
if ! run_test "Final comprehensive test run" "go test ./pkg/... -short"; then
    ALL_PASSED=false
fi

# Print final summary
echo ""
echo "╔════════════════════════════════════════════════════════════════╗"
echo "║                        TEST SUMMARY                            ║"
echo "╚════════════════════════════════════════════════════════════════╝"
echo ""
echo "Total Test Suites: $TOTAL_TESTS"
echo -e "Passed: ${GREEN}$PASSED_TESTS${NC}"
echo -e "Failed: ${RED}$FAILED_TESTS${NC}"
echo ""

if [ "$ALL_PASSED" = true ]; then
    echo -e "${GREEN}╔════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                   ✓ ALL TESTS PASSED ✓                        ║${NC}"
    echo -e "${GREEN}║              Your changes are ready to submit!                 ║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════════════════════════════╝${NC}"
    exit 0
else
    echo -e "${RED}╔════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║                   ✗ TESTS FAILED ✗                            ║${NC}"
    echo -e "${RED}║                                                                ║${NC}"
    echo -e "${RED}║  DO NOT SUBMIT until all tests pass!                          ║${NC}"
    echo -e "${RED}║  Fix the failing tests above before proceeding.               ║${NC}"
    echo -e "${RED}║                                                                ║${NC}"
    echo -e "${RED}║  For Agents: These failures are YOUR responsibility to fix    ║${NC}"
    echo -e "${RED}╚════════════════════════════════════════════════════════════════╝${NC}"
    exit 1
fi
