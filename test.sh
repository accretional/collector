# Generate proto files first
protoc --go_out=./gen --go_opt=paths=source_relative \
    --go-grpc_out=./gen --go-grpc_opt=paths=source_relative \
    proto/*.proto

# Run main
go run cmd/main.go
