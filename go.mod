module github.com/accretional/collector

go 1.24.0

toolchain go1.24.10

require (
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/golang/protobuf v1.5.4
	github.com/google/uuid v1.6.0
	github.com/mattn/go-sqlite3 v1.14.32
	google.golang.org/grpc v1.78.0
	google.golang.org/protobuf v1.36.11
)

replace github.com/accretional/collector/gen/collector => ./gen/collector

require (
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251029180050-ab9386a59fda // indirect
)
