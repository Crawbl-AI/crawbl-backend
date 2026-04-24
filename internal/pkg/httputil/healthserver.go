package httputil

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// NewHealthServer creates a health-only HTTP server.
func NewHealthServer(cfg *HealthConfig, logger *slog.Logger) *HealthServer {
	port := cfg.Port
	if port == "" {
		port = DefaultHealthPort
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return &HealthServer{
		httpServer: &http.Server{
			Addr:              ":" + port,
			Handler:           mux,
			ReadHeaderTimeout: DefaultReadHeaderTimeout,
		},
		logger: logger,
	}
}

// Run starts the health server and blocks until ctx is cancelled.
func (s *HealthServer) Run(ctx context.Context, shutdownTimeout time.Duration) error {
	return ListenAndServeGraceful(ctx, s.httpServer, shutdownTimeout, "health", s.logger)
}
