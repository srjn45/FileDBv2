package server

import (
	"context"
	"net"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/srjn45/filedbv2/internal/pb/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// NewRESTGateway returns an http.Handler that proxies requests to the gRPC
// server listening on grpcAddr via the grpc-gateway.
func NewRESTGateway(ctx context.Context, grpcAddr string) (http.Handler, error) {
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

	if err := pb.RegisterFileDBHandlerFromEndpoint(ctx, mux, grpcAddr, opts); err != nil {
		return nil, err
	}
	return mux, nil
}

// NewRESTGatewayUnix returns an http.Handler that dials the gRPC server via a
// Unix domain socket.
func NewRESTGatewayUnix(ctx context.Context, socketPath string) (http.Handler, error) {
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}),
	}
	if err := pb.RegisterFileDBHandlerFromEndpoint(ctx, mux, "unix://"+socketPath, opts); err != nil {
		return nil, err
	}
	return mux, nil
}
