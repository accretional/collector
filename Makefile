.PHONY: hot build test clean 

hot:
	go run -tags sqlite_fts5 ./cmd/server/main.go

build:
	go build -tags sqlite_fts5 -o bin/collector ./cmd/server

vet:
	go vet -tags sqlite_fts5 ./...

clean:
	rm -rf bin/
	rm -rf data/

lint: vet fmt

.DEFAULT_GOAL := hot
