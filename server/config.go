package server

import (
	"time"

	"github.com/srjn45/filedbv2/internal/engine"
)

// Config holds all server configuration, loaded from CLI flags → env vars →
// config file, in priority order.
type Config struct {
	// Storage
	DataDir string // default: ./data

	// Network
	GRPCAddr   string // default: :5433
	RESTAddr   string // default: :8080
	UnixSocket string // default: /tmp/filedb.sock

	// Auth
	APIKey string // empty = no auth

	// Engine tuning
	SegmentMaxSize  int64         // default: 4 MiB
	CompactInterval time.Duration // default: 5m
	CompactDirtyPct float64       // default: 0.30
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		DataDir:         "./data",
		GRPCAddr:        ":5433",
		RESTAddr:        ":8080",
		UnixSocket:      "/tmp/filedb.sock",
		SegmentMaxSize:  4 * 1024 * 1024,
		CompactInterval: 5 * time.Minute,
		CompactDirtyPct: 0.30,
	}
}

// EngineConfig converts server config into an engine.CollectionConfig.
func (c Config) EngineConfig() engine.CollectionConfig {
	return engine.CollectionConfig{
		SegmentMaxSize:  c.SegmentMaxSize,
		CompactInterval: c.CompactInterval,
		CompactDirtyPct: c.CompactDirtyPct,
	}
}
