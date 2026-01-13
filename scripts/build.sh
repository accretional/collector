#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "================================================"
echo "Collector Setup Script"
echo "================================================"
echo ""

sudo apt-get update -qq
sudo apt-get install -y -qq build-essential libsqlite3-dev protobuf-compiler
echo "System dependencies installed"
echo ""

go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
export PATH="$PATH:$(go env GOPATH)/bin"
echo "Go plugins installed"
echo ""

cd "$PROJECT_ROOT"
for f in proto/*.proto; do
    if ! grep -q "option go_package" "$f"; then
        echo "   Adding go_package to $f"
        sed -i 's/package collector;/package collector;\noption go_package = "github.com\/accretional\/collector\/gen\/collector";/' "$f"
    fi
done
echo "Proto definitions ready"
echo ""

bash "$SCRIPT_DIR/generate-proto.sh"
echo ""

go mod tidy
echo "Go modules synced"
echo ""

CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
echo "Build successful"
echo ""

echo "================================================"
echo "Setup complete!"
echo "================================================"
echo ""
echo "Next steps:"
echo "  Start the server:  CGO_ENABLED=1 go run -tags sqlite_fts5 cmd/server/main.go"
echo "  Run tests:         CGO_ENABLED=1 go test -tags sqlite_fts5 ./..."
echo ""
