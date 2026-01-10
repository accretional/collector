package main

import (
	"context"
	"log"
	"net"
	"net/netip"
	"runtime/debug"
	"syscall"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/realip"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/testing/testpb"
	"github.com/oklog/run"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	TestServerAddr = "localhost:50052"
)

// interceptorLogger adapts standard log to interceptor logger
func interceptorLogger() logging.Logger {
	return logging.LoggerFunc(func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
		log.Printf("[%s] %s %v", lvl, msg, fields)
	})
}

func main() {
	logger := log.Default()

	grpcPanicRecoveryHandler := func(p any) (err error) {
		logger.Printf("recovered from panic: %v\n%s", p, debug.Stack())
		return status.Errorf(codes.Internal, "panic triggered: %v", p)
	}

	trustedPeers := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("0.0.0.0/0"), // Allow all for testing
	}
	realIPOpts := []realip.Option{
		realip.WithTrustedPeers(trustedPeers),
		realip.WithHeaders([]string{realip.XForwardedFor, realip.XRealIp}),
	}

	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			realip.UnaryServerInterceptorOpts(realIPOpts...),

			logging.UnaryServerInterceptor(
				interceptorLogger(),
				logging.WithLogOnEvents(logging.StartCall, logging.FinishCall),
			),

			recovery.UnaryServerInterceptor(recovery.WithRecoveryHandler(grpcPanicRecoveryHandler)),
		),
		grpc.ChainStreamInterceptor(
			realip.StreamServerInterceptorOpts(realIPOpts...),
			logging.StreamServerInterceptor(
				interceptorLogger(),
				logging.WithLogOnEvents(logging.StartCall, logging.FinishCall),
			),
			recovery.StreamServerInterceptor(recovery.WithRecoveryHandler(grpcPanicRecoveryHandler)),
		),
	)

	t := &testpb.TestPingService{}
	testpb.RegisterTestServiceServer(grpcSrv, t)

	lis, err := net.Listen("tcp", TestServerAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	logger.Printf("Test gRPC server listening on %s", TestServerAddr)
	logger.Println("Server middleware enabled:")
	logger.Println("  - RealIP extraction")
	logger.Println("  - Structured logging")
	logger.Println("  - Panic recovery")
	logger.Println("\nPress Ctrl+C to shutdown")

	g := &run.Group{}
	g.Add(func() error {
		return grpcSrv.Serve(lis)
	}, func(err error) {
		logger.Println("Shutting down server...")
		grpcSrv.GracefulStop()
		grpcSrv.Stop()
	})

	g.Add(run.SignalHandler(context.Background(), syscall.SIGINT, syscall.SIGTERM))

	if err := g.Run(); err != nil {
		logger.Printf("Server error: %v", err)
	}
}
