// Package healthserver provides a minimal HTTP server with only a
// /health endpoint for Kubernetes liveness and readiness probes.
// Used by components that process no HTTP traffic (e.g. the River
// background worker).
package healthserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httprun"
)

const (
	DefaultPort              = "7175"
	DefaultReadHeaderTimeout = 5 * time.Second
)

// Config holds health server configuration.
type Config struct {
	Port string
}

// Server is a minimal HTTP server serving only /health.
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// New creates a health-only HTTP server.
func New(cfg *Config, logger *slog.Logger) *Server {
	port := cfg.Port
	if port == "" {
		port = DefaultPort
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return &Server{
		httpServer: &http.Server{
			Addr:              ":" + port,
			Handler:           mux,
			ReadHeaderTimeout: DefaultReadHeaderTimeout,
		},
		logger: logger,
	}
}

// Run starts the health server and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context, shutdownTimeout time.Duration) error {
	return httprun.ListenAndServeGraceful(ctx, s.httpServer, shutdownTimeout, "health", s.logger)
}
