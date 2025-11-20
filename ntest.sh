#!/bin/bash

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

# 5. Run Main
echo "🚀 Running application..."
go run cmd/main.go

# 6. Run Tests
echo "🧪 Running tests..."
go test -v ./pkg/collection/ -run Durability
go test -v ./pkg/collection/ -run Recovery
go test -v ./pkg/collection/ -run Concurrent
go test -v ./pkg/collection/ -run Stress

# Run with race detector
go test -race -v ./pkg/collection/ -run Concurrent

# Run benchmarks
go test -bench=. ./pkg/collection/ -benchtime=5s

# Run all tests with coverage
go test -v -race -coverprofile=coverage.out ./pkg/collection/...
go tool cover -func=coverage.out
