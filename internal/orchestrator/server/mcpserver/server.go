// Package mcpserver provides the standalone MCP HTTP server for the
// decomposed orchestrator. It mounts the MCP handler at /mcp/v1 and
// a health endpoint at /health for Kubernetes probes.
package mcpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/httputil"
)

const (
	DefaultPort              = "7172"
	DefaultReadHeaderTimeout = 10 * time.Second
	DefaultReadTimeout       = 30 * time.Second
	DefaultWriteTimeout      = 90 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultMaxHeaderBytes    = 1 << 20
)

// Config holds MCP server configuration.
type Config struct {
	Port string
}

// Server is the standalone MCP HTTP server.
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// New creates the MCP server mounting the handler at /mcp/v1 with a
// /health probe endpoint.
func New(cfg *Config, mcpHandler http.Handler, logger *slog.Logger) *Server {
	port := cfg.Port
	if port == "" {
		port = DefaultPort
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp/v1", mcpHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return &Server{
		httpServer: &http.Server{
			Addr:              ":" + port,
			Handler:           mux,
			ReadHeaderTimeout: DefaultReadHeaderTimeout,
			ReadTimeout:       DefaultReadTimeout,
			WriteTimeout:      DefaultWriteTimeout,
			IdleTimeout:       DefaultIdleTimeout,
			MaxHeaderBytes:    DefaultMaxHeaderBytes,
		},
		logger: logger,
	}
}

// Run starts the server and blocks until ctx is cancelled, then
// performs a graceful shutdown within the given timeout.
func (s *Server) Run(ctx context.Context, shutdownTimeout time.Duration) error {
	return httputil.ListenAndServeGraceful(ctx, s.httpServer, shutdownTimeout, "MCP", s.logger)
}
