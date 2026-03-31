package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	pb "github.com/srjn45/filedbv2/internal/pb/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
)

// watchDialOpts bundles the address and dial options needed to reach the gRPC
// server from the custom Watch HTTP handler.
type watchDialOpts struct {
	addr string
	opts []grpc.DialOption
}

// watchInterceptor wraps next with a handler for POST /v1/{collection}/watch.
// All other requests are forwarded to next unchanged.
//
// The grpc-gateway generator does not emit a handler for the Watch RPC; this
// custom handler fills that gap by dialling back to the gRPC server and
// streaming the response as newline-delimited JSON in the grpc-gateway envelope
// format:
//
//	{"result":<WatchEvent JSON>}\n
func watchInterceptor(next http.Handler, dial watchDialOpts) http.Handler {
	// I1: Create the gRPC connection once at construction time and share it
	// across all requests via closure.
	conn, err := grpc.NewClient(dial.addr, dial.opts...)
	if err != nil {
		// If we cannot dial at startup, fall back to a handler that always
		// returns an error rather than panicking.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || !isWatchPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "watch: dial: "+err.Error(), http.StatusBadGateway)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !isWatchPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Extract collection name from /v1/{collection}/watch.
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		collection := parts[1]

		// C1: Decode the JSON request body to extract an optional filter.
		req := &pb.WatchRequest{Collection: collection}
		if r.Body != nil {
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				http.Error(w, "watch: read body: "+readErr.Error(), http.StatusBadRequest)
				return
			}
			if len(body) > 0 {
				if unmarshalErr := protojson.Unmarshal(body, req); unmarshalErr != nil {
					http.Error(w, "watch: decode body: "+unmarshalErr.Error(), http.StatusBadRequest)
					return
				}
				// Restore the collection from the URL path; the body may or may
				// not include it and the path is authoritative.
				req.Collection = collection
			}
		}

		// Propagate the API key as gRPC metadata.
		apiKey := r.Header.Get("x-api-key")
		ctx := metadata.NewOutgoingContext(r.Context(), metadata.Pairs("x-api-key", apiKey))

		stream, err := pb.NewFileDBClient(conn).Watch(ctx, req)
		if err != nil {
			http.Error(w, "watch: start: "+err.Error(), http.StatusBadGateway)
			return
		}

		flusher, canFlush := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering if present
		w.WriteHeader(http.StatusOK)

		enc := json.NewEncoder(w)
		for {
			event, recvErr := stream.Recv()
			if recvErr != nil {
				if errors.Is(recvErr, io.EOF) {
					// I4: Normal stream end — return silently.
					return
				}
				// I4: Non-EOF error — write an error envelope before closing.
				_ = enc.Encode(map[string]any{
					"error": map[string]any{
						"message": recvErr.Error(),
						"code":    int(codes.Internal),
					},
				})
				if canFlush {
					flusher.Flush()
				}
				return
			}
			// Wrap in the grpc-gateway envelope so the web client can use the
			// same parsing logic as all other streaming endpoints.
			if encErr := enc.Encode(map[string]any{"result": event}); encErr != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
	})
}

// isWatchPath reports whether path matches /v1/<collection>/watch.
func isWatchPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 3 && parts[0] == "v1" && parts[2] == "watch"
}

// newTCPWatchDial returns watchDialOpts for dialling via TCP.
func newTCPWatchDial(addr string, creds credentials.TransportCredentials) watchDialOpts {
	return watchDialOpts{
		addr: addr,
		opts: []grpc.DialOption{grpc.WithTransportCredentials(creds)},
	}
}

// newUnixWatchDial returns watchDialOpts for dialling via a Unix domain socket.
func newUnixWatchDial(socketPath string) watchDialOpts {
	return watchDialOpts{
		addr: "unix://" + socketPath,
		opts: []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			}),
		},
	}
}
