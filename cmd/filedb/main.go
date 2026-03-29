package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/srjn45/filedbv2/internal/auth"
	"github.com/srjn45/filedbv2/internal/engine"
	pb "github.com/srjn45/filedbv2/internal/pb/proto"
	"github.com/srjn45/filedbv2/server"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "filedb",
		Short: "FileDB — lightweight append-only file database",
	}
	root.AddCommand(serveCmd())
	return root
}

func serveCmd() *cobra.Command {
	cfg := server.DefaultConfig()

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the FileDB server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serve(cfg)
		},
	}

	f := cmd.Flags()
	f.StringVar(&cfg.DataDir, "data", cfg.DataDir, "Data directory")
	f.StringVar(&cfg.GRPCAddr, "grpc-addr", cfg.GRPCAddr, "gRPC listen address")
	f.StringVar(&cfg.RESTAddr, "rest-addr", cfg.RESTAddr, "REST listen address")
	f.StringVar(&cfg.UnixSocket, "socket", cfg.UnixSocket, "Unix socket path")
	f.StringVar(&cfg.APIKey, "api-key", os.Getenv("FILEDB_API_KEY"), "API key (env: FILEDB_API_KEY)")
	f.Int64Var(&cfg.SegmentMaxSize, "segment-size", cfg.SegmentMaxSize, "Max segment file size in bytes")
	f.DurationVar(&cfg.CompactInterval, "compact-interval", cfg.CompactInterval, "Compaction interval")
	f.Float64Var(&cfg.CompactDirtyPct, "compact-dirty", cfg.CompactDirtyPct, "Dirty ratio threshold to trigger compaction (0–1)")

	return cmd
}

func serve(cfg server.Config) error {
	// Open the database.
	db, err := engine.Open(cfg.DataDir, cfg.EngineConfig())
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()
	log.Printf("filedb: data dir=%q", cfg.DataDir)

	// Build gRPC server with auth interceptors.
	unary, stream := auth.Interceptors(cfg.APIKey)
	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(unary),
		grpc.StreamInterceptor(stream),
	)
	pb.RegisterFileDBServer(grpcSrv, server.NewGRPCServer(db))

	// TCP listener for gRPC.
	tcpLn, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("grpc tcp listen %q: %w", cfg.GRPCAddr, err)
	}
	log.Printf("filedb: gRPC listening on %s", cfg.GRPCAddr)

	// Unix socket listener for gRPC (local connections).
	_ = os.Remove(cfg.UnixSocket)
	unixLn, err := net.Listen("unix", cfg.UnixSocket)
	if err != nil {
		log.Printf("filedb: unix socket unavailable (%v), skipping", err)
	} else {
		log.Printf("filedb: gRPC unix socket at %s", cfg.UnixSocket)
		go func() { _ = grpcSrv.Serve(unixLn) }()
	}

	// REST gateway.
	ctx, cancelGW := context.WithCancel(context.Background())
	defer cancelGW()

	restHandler, err := server.NewRESTGateway(ctx, cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("rest gateway: %w", err)
	}
	restSrv := &http.Server{Addr: cfg.RESTAddr, Handler: restHandler}
	go func() {
		log.Printf("filedb: REST listening on %s", cfg.RESTAddr)
		if err := restSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("filedb: REST server error: %v", err)
		}
	}()

	// Start gRPC (TCP).
	go func() { _ = grpcSrv.Serve(tcpLn) }()

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("filedb: shutting down...")

	grpcSrv.GracefulStop()

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = restSrv.Shutdown(shutCtx)

	if unixLn != nil {
		_ = os.Remove(cfg.UnixSocket)
	}

	log.Println("filedb: stopped")
	return nil
}
