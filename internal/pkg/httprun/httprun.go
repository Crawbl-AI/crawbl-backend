// Package httprun provides a shared helper for running an HTTP server with
// graceful shutdown tied to a context lifetime.
package httprun

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// ListenAndServeGraceful starts srv and blocks until ctx is cancelled, then
// performs a graceful shutdown within shutdownTimeout. serverName is used only
// for log messages (e.g. "health", "MCP").
func ListenAndServeGraceful(ctx context.Context, srv *http.Server, shutdownTimeout time.Duration, serverName string, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting "+serverName+" server", slog.String("addr", srv.Addr))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// ctx is already cancelled here, so create a fresh background context
		// for the shutdown timeout. This ensures shutdown has its own deadline.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		logger.Info("shutting down " + serverName + " server")
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	}
}
