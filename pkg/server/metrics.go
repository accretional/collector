package server

import (
	"context"

	"google.golang.org/grpc"
)

func MetricsInterceptor(m *Metrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		m.RecordRequest()
		resp, err := handler(ctx, info)
		m.RecordRequestDone(err)
		return resp, err
	}
}
