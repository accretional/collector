#!/bin/bash
# Generate proto files first
protoc -I proto --go_out=./gen --go_opt=paths=source_relative \
    --go-grpc_out=./gen --go-grpc_opt=paths=source_relative \
    proto/*.proto

# Run main
go run cmd/main.go

# Run all durability tests
go test -tags sqlite_fts5 -v ./pkg/collection/ -run Durability
go test -tags sqlite_fts5 -v ./pkg/collection/ -run Recovery
go test -tags sqlite_fts5 -v ./pkg/collection/ -run Concurrent
go test -tags sqlite_fts5 -v ./pkg/collection/ -run Stress

# Run with race detector (important for concurrency tests)
go test -tags sqlite_fts5 -race -v ./pkg/collection/ -run Concurrent

# Run stress tests (skipped by default)
go test -tags sqlite_fts5 -v ./pkg/collection/ -run Stress -timeout 5m

# Run benchmarks
go test -tags sqlite_fts5 -bench=. ./pkg/collection/ -benchtime=5s

# Run all tests with coverage
go test -tags sqlite_fts5 -v -race -coverprofile=coverage.out ./pkg/collection/...
go tool cover -func=coverage.out
