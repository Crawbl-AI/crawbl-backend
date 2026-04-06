package grpc

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
)

// GracefulShutdown initiates a grpc.Server.GracefulStop bounded by
// the provided timeout. If graceful stop does not complete within the
// timeout window, it forces a hard Stop() so the process can exit
// cleanly under a shutdown deadline.
//
// Typical use in main.go:
//
//	sig := make(chan os.Signal, 1)
//	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
//	<-sig
//	crawblgrpc.GracefulShutdown(srv, 30*time.Second, logger)
//
// This centralizes the graceful-stop dance every gRPC server in
// crawbl-backend needs, so individual server packages don't
// re-implement the timer + force-close pattern.
func GracefulShutdown(srv *grpc.Server, timeout time.Duration, logger *slog.Logger) {
	if srv == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	if timeout <= 0 {
		timeout = keepaliveIntervalSeconds * time.Second
	}
	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		logger.Info("gRPC server stopped gracefully")
	case <-time.After(timeout):
		logger.Warn("gRPC graceful shutdown timed out, forcing stop", "timeout", timeout)
		srv.Stop()
	}
}

// ShutdownContext is a variant of GracefulShutdown that watches a
// caller-provided context instead of taking a wall-clock timeout. Use
// when the shutdown deadline is carried by the parent process's
// context (e.g. signal handler that wraps context.WithTimeout).
func ShutdownContext(ctx context.Context, srv *grpc.Server, logger *slog.Logger) {
	if srv == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		logger.Info("gRPC server stopped gracefully")
	case <-ctx.Done():
		logger.Warn("gRPC shutdown context cancelled, forcing stop", "reason", ctx.Err())
		srv.Stop()
	}
}
