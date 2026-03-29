package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/srjn45/filedbv2/internal/pb/proto"
)

type cliFlags struct {
	host   string
	socket string
	apiKey string
}

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	flags := &cliFlags{}

	root := &cobra.Command{
		Use:   "filedb-cli",
		Short: "FileDB command-line client",
	}

	pf := root.PersistentFlags()
	pf.StringVar(&flags.host, "host", "localhost:5433", "FileDB gRPC address")
	pf.StringVar(&flags.socket, "socket", "/tmp/filedb.sock", "Unix socket path (used if socket file exists)")
	pf.StringVar(&flags.apiKey, "api-key", os.Getenv("FILEDB_API_KEY"), "API key (env: FILEDB_API_KEY)")

	root.AddCommand(
		replCmd(flags),
		runCmd(flags),
		insertCmd(flags),
		findCmd(flags),
		findByIDCmd(flags),
		updateCmd(flags),
		deleteCmd(flags),
		collectionsCmd(flags),
		createCollectionCmd(flags),
		dropCollectionCmd(flags),
		statsCmd(flags),
		exportCmd(flags),
		importCmd(flags),
		beginTxCmd(flags),
		commitTxCmd(flags),
		rollbackTxCmd(flags),
	)
	return root
}

// connect dials the FileDB server, preferring the Unix socket when available.
func connect(flags *cliFlags) (*grpc.ClientConn, pb.FileDBClient, func(), error) {
	var (
		conn *grpc.ClientConn
		err  error
	)

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	// Prefer Unix socket if the file exists.
	if _, statErr := os.Stat(flags.socket); statErr == nil {
		opts = append(opts, grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", flags.socket)
		}))
		conn, err = grpc.NewClient("unix://"+flags.socket, opts...)
	} else {
		conn, err = grpc.NewClient(flags.host, opts...)
	}

	if err != nil {
		return nil, nil, nil, fmt.Errorf("connect: %w", err)
	}

	client := pb.NewFileDBClient(conn)
	cleanup := func() { _ = conn.Close() }
	return conn, client, cleanup, nil
}

// ctxWithAuth returns a context carrying the API key metadata.
func ctxWithAuth(flags *cliFlags) context.Context {
	if flags.apiKey == "" {
		return context.Background()
	}
	return metadata.NewOutgoingContext(
		context.Background(),
		metadata.Pairs("x-api-key", flags.apiKey),
	)
}
