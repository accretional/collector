package main

import (
	"context"
	"log"
	"time"

	pb "github.com/accretional/collector/gen/collector"
	"github.com/accretional/collector/pkg/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// This example shows how to embed the Collector server in your own application.
func main() {
	// Create and start the Collector server
	srv, err := server.New(server.Config{
		DataDir:   "./my-app-data",
		Port:      8080,
		Namespace: "my-app",
		// CollectorID will auto-generate a UUID7 if not specified
		// Set it explicitly if you need a stable ID across restarts
	})
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	log.Printf("Collector server started at %s", srv.Address())
	log.Printf("Collector ID: %s", srv.CollectorID())
	log.Printf("Namespace: %s", srv.Namespace())

	// Server is already running, do your application initialization here

	// Example: Connect to the embedded server and create a collection
	conn, err := grpc.NewClient(
		srv.Address(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewCollectionRepoClient(conn)

	// Create a collection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.CreateCollection(ctx, &pb.CreateCollectionRequest{
		Collection: &pb.Collection{
			Namespace: "my-app",
			Name:      "users",
			MessageType: &pb.MessageTypeRef{
				MessageName: "User",
			},
		},
	})
	if err != nil {
		log.Printf("Failed to create collection: %v", err)
	} else {
		log.Printf("Collection created: %s", resp.CollectionId)
	}

	// Your application logic here...
	log.Println("Application running with embedded Collector")

	// Wait for shutdown signal
	srv.WaitForShutdown()
}
