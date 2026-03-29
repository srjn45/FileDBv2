// Package auth provides gRPC interceptors for API key authentication.
package auth

import (
	"context"
	"crypto/subtle"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const metadataKey = "x-api-key"

// Interceptors returns a pair of gRPC interceptors (unary + stream) that
// validate the x-api-key metadata header against the given expected key.
// Pass an empty expectedKey to disable authentication entirely.
func Interceptors(expectedKey string) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor) {
	if expectedKey == "" {
		return noopUnary, noopStream
	}
	expected := []byte(expectedKey)
	return unaryInterceptor(expected), streamInterceptor(expected)
}

func unaryInterceptor(expected []byte) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if err := validate(ctx, expected); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func streamInterceptor(expected []byte) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		_ *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if err := validate(ss.Context(), expected); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

func validate(ctx context.Context, expected []byte) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	vals := md.Get(metadataKey)
	if len(vals) == 0 {
		return status.Errorf(codes.Unauthenticated, "missing %s", metadataKey)
	}
	provided := []byte(vals[0])
	if subtle.ConstantTimeCompare(provided, expected) != 1 {
		return status.Error(codes.Unauthenticated, "invalid api key")
	}
	return nil
}

func noopUnary(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	return handler(ctx, req)
}

func noopStream(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	return handler(srv, ss)
}
