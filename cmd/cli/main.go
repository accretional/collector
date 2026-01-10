package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/retry"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/timeout"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/testing/testpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	DefaultEndpoint = "localhost:50052"
)

// interceptorLogger adapts standard log to interceptor logger
func interceptorLogger() logging.Logger {
	return logging.LoggerFunc(func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
		log.Printf("[%s] %s %v", lvl, msg, fields)
	})
}

func newClient(endpoint string) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),

		grpc.WithUnaryInterceptor(logging.UnaryClientInterceptor(
			interceptorLogger(),
			logging.WithLogOnEvents(logging.StartCall, logging.FinishCall),
		)),

		grpc.WithUnaryInterceptor(retry.UnaryClientInterceptor(
			retry.WithMax(3),
			retry.WithPerRetryTimeout(2*time.Second),
			retry.WithBackoff(retry.BackoffExponential(100*time.Millisecond)),
		)),

		grpc.WithUnaryInterceptor(timeout.UnaryClientInterceptor(30 * time.Second)),

		grpc.WithStreamInterceptor(logging.StreamClientInterceptor(
			interceptorLogger(),
			logging.WithLogOnEvents(logging.StartCall, logging.FinishCall),
		)),
		grpc.WithStreamInterceptor(retry.StreamClientInterceptor(
			retry.WithMax(3),
			retry.WithPerRetryTimeout(2*time.Second),
		)),
	}

	conn, err := grpc.NewClient(endpoint, opts...)
	if err != nil {
		return nil, err
	}

	log.Printf("Connected to %s with client middleware enabled", endpoint)
	return conn, nil
}

func main() {
	endpoint := flag.String("endpoint", DefaultEndpoint, "gRPC server endpoint")
	flag.Parse()

	conn, err := newClient(*endpoint)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := testpb.NewTestServiceClient(conn)

	ctx := context.Background()

	log.Println("Testing unary call...")
	resp, err := client.Ping(ctx, &testpb.PingRequest{Value: "test from CLI"})
	if err != nil {
		log.Fatalf("Ping failed: %v", err)
	}
	log.Printf("✓ Unary call success! Response: %s", resp.Value)

	log.Println("Testing empty call...")
	emptyResp, err := client.PingEmpty(ctx, &testpb.PingEmptyRequest{})
	if err != nil {
		log.Fatalf("PingEmpty failed: %v", err)
	}
	log.Printf("✓ Empty call success! Response: %+v", emptyResp)

	log.Println("Testing streaming call...")
	stream, err := client.PingList(ctx, &testpb.PingListRequest{Value: "stream test"})
	if err != nil {
		log.Fatalf("PingList failed: %v", err)
	}

	count := 0
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			log.Fatalf("Stream recv failed: %v", err)
		}
		count++
		if count <= 3 {
			log.Printf("  Received: %s (counter: %d)", msg.Value, msg.Counter)
		}
	}
	log.Printf("✓ Streaming call success! Received %d messages", count)

	log.Println("\nAll tests passed! Client middleware is working correctly.")
}
