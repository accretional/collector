#!/bin/bash
# Generate proto files first
protoc --go_out=./gen --go_opt=paths=source_relative \
    --go-grpc_out=./gen --go-grpc_opt=paths=source_relative \
    proto/*.proto

# Run main
go run cmd/main.go

# Run all durability tests
go test -v ./pkg/collection/ -run Durability
go test -v ./pkg/collection/ -run Recovery
go test -v ./pkg/collection/ -run Concurrent
go test -v ./pkg/collection/ -run Stress

# Run with race detector (important for concurrency tests)
go test -race -v ./pkg/collection/ -run Concurrent

# Run stress tests (skipped by default)
go test -v ./pkg/collection/ -run Stress -timeout 5m

# Run benchmarks
go test -bench=. ./pkg/collection/ -benchtime=5s

# Run all tests with coverage
go test -v -race -coverprofile=coverage.out ./pkg/collection/...
go tool cover -func=coverage.out
