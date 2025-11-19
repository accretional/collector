#!/bin/bash
# Generate proto files first
protoc --go_out=./gen --go_opt=paths=source_relative \
    --go-grpc_out=./gen --go-grpc_opt=paths=source_relative \
    proto/*.proto

# Run main
go run cmd/main.go

echo "Running all collection tests..."
echo "================================"
echo

# Run tests with coverage
go test -v -race -coverprofile=coverage.out ./pkg/collection/...

# Show coverage
echo
echo "Coverage Summary:"
echo "================"
go tool cover -func=coverage.out | grep total

# Optional: Generate HTML coverage report
# go tool cover -html=coverage.out -o coverage.html
# echo "HTML coverage report generated: coverage.html"
