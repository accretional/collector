package main

import (
	"log"
	"os"
	"strconv"

	"github.com/accretional/collector/pkg/server"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Configuration from environment variables
	dataDir := os.Getenv("COLLECTOR_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	port := 50051
	if portStr := os.Getenv("PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	namespace := os.Getenv("COLLECTOR_NAMESPACE")
	if namespace == "" {
		namespace = "shared"
	}

	collectorID := os.Getenv("COLLECTOR_ID")
	// If not set, server.New() will generate a random UUID7

	// Create and start server
	srv, err := server.New(server.Config{
		DataDir:     dataDir,
		Port:        port,
		Namespace:   namespace,
		CollectorID: collectorID,
	})
	if err != nil {
		return err
	}
	defer srv.Close()

	if err := srv.Start(); err != nil {
		return err
	}

	// Wait for shutdown signal
	srv.WaitForShutdown()

	return nil
}
