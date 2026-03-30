// Package server provides the HTTP server implementation for the Crawbl orchestrator API.
// It handles authentication, workspace management, and chat operations for the mobile-facing
// HTTP interface that sits between the Flutter app and user ZeroClaw swarms.
//
// The server package contains:
//   - HTTP server initialization and lifecycle management
//   - Request handlers for auth, user, workspace, and chat endpoints
//   - Request and response type definitions for the API
//   - Utility functions for request processing and response formatting
//
// Key design principles:
//   - The server acts as the control plane, not a thin API wrapper
//   - Each request gets its own database session for transaction isolation
//   - All endpoints require authentication via Firebase tokens or dev tokens
//   - Responses follow a consistent JSON structure with success/error envelopes
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// NewServer creates a new orchestrator Server instance with the provided configuration and options.
// It validates all required dependencies and initializes the HTTP server with registered routes.
// The function panics if any required configuration or dependency is missing.
//
// Parameters:
//   - config: Server configuration including port settings
//   - opts: Server dependencies including database, logger, services, and middleware
//
// Returns a fully initialized Server ready to accept connections.
func NewServer(config *Config, opts *NewServerOpts) *Server {
	validateNewServer(config, opts)

	broadcaster := opts.Broadcaster
	if broadcaster == nil {
		broadcaster = realtime.NopBroadcaster{}
	}

	srv := &Server{
		db:                 opts.DB,
		logger:             opts.Logger,
		authService:        opts.AuthService,
		workspaceService:   opts.WorkspaceService,
		chatService:        opts.ChatService,
		integrationService: opts.IntegrationService,
		httpMiddleware:     opts.HTTPMiddleware,
		broadcaster:        broadcaster,
		runtimeClient:      opts.RuntimeClient,
		cfg:                config,
	}

	// Build the combined handler: MCP + Socket.IO (if provided) + chi REST router.
	// Each sub-handler owns its own path prefix. We use a ServeMux so each
	// handler intercepts its own requests while chi handles everything else.
	handler := registerRoutes(srv)
	if opts.SocketIOHandler != nil || opts.MCPHandler != nil {
		mux := http.NewServeMux()
		if opts.SocketIOHandler != nil {
			mux.Handle("/socket.io/", opts.SocketIOHandler)
		}
		if opts.MCPHandler != nil {
			mux.Handle("/mcp/v1", opts.MCPHandler)
		}
		mux.Handle("/", handler)
		handler = mux
	}

	srv.httpServer = &http.Server{
		Addr:              ":" + config.Port,
		Handler:           handler,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
	}

	return srv
}

// ListenAndServe starts the HTTP server and begins accepting connections.
// It blocks until the server encounters an error other than http.ErrServerClosed.
// Use Shutdown for graceful termination.
//
// Returns an error if the server fails to start or encounters a fatal error
// during operation, excluding intentional shutdown.
func (s *Server) ListenAndServe() error {
	s.logger.Info("starting orchestrator server", slog.String("addr", s.httpServer.Addr))
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Run ties the HTTP server lifecycle to a caller-provided context.
//
// This keeps signal handling outside the package while still giving the server
// one obvious entrypoint for "start now, then shut down gracefully when the
// command context is canceled".
func (s *Server) Run(ctx context.Context, shutdownTimeout time.Duration) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := s.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	}
}

// Shutdown gracefully stops the HTTP server, allowing in-flight requests
// to complete up to the context deadline. Call this method for clean server
// termination instead of abruptly closing connections.
//
// Parameters:
//   - ctx: Context with deadline for shutdown timeout
//
// Returns an error if the shutdown fails or times out.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// validateNewServer checks that all required server configuration and options are present.
// It panics with a descriptive message if any required field is nil or empty.
// This fail-fast behavior ensures configuration errors are caught at startup.
//
//nolint:cyclop
func validateNewServer(config *Config, opts *NewServerOpts) {
	if config == nil || opts == nil {
		panic("server config and options are required")
	}
	if config.Port == "" {
		panic("server port is required")
	}
	if opts.Logger == nil {
		panic("logger is required")
	}
	if opts.DB == nil {
		panic("database connection is required")
	}
	if opts.AuthService == nil {
		panic("auth service is required")
	}
	if opts.WorkspaceService == nil {
		panic("workspace service is required")
	}
	if opts.ChatService == nil {
		panic("chat service is required")
	}
	if opts.HTTPMiddleware == nil {
		panic("http middleware config is required")
	}
}
