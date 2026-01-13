#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

export PATH="$PATH:$(go env GOPATH)/bin"

rm -rf "$PROJECT_ROOT/gen"
mkdir -p "$PROJECT_ROOT/gen/collector"

cd "$PROJECT_ROOT/proto"
protoc --go_out="$PROJECT_ROOT/gen/collector" --go_opt=paths=source_relative \
    --go-grpc_out="$PROJECT_ROOT/gen/collector" --go-grpc_opt=paths=source_relative \
    *.proto

echo "Generated protobuf in gen/collector/"
