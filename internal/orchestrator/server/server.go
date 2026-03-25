package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
)

func NewServer(config *Config, opts *NewServerOpts) *Server {
	validateNewServer(config, opts)

	srv := &Server{
		db:               opts.DB,
		logger:           opts.Logger,
		authService:      opts.AuthService,
		workspaceService: opts.WorkspaceService,
		chatService:      opts.ChatService,
		httpMiddleware:   opts.HTTPMiddleware,
	}

	srv.httpServer = &http.Server{
		Addr:              ":" + config.Port,
		Handler:           registerRoutes(srv),
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
	}

	return srv
}

func (s *Server) ListenAndServe() error {
	s.logger.Info("starting orchestrator server", slog.String("addr", s.httpServer.Addr))
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

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
	if opts.HTTPMiddleware.IdentityVerifier == nil {
		panic("identity verifier is required")
	}
}
