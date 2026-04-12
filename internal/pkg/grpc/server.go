package grpc

import (
	"context"
	"log/slog"
	"strings"
	"time"

	grpcauth "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

// ---------------------------------------------------------------------------
// Graceful shutdown
// ---------------------------------------------------------------------------

// GracefulShutdown initiates a grpc.Server.GracefulStop bounded by the
// provided timeout. If graceful stop does not complete within the timeout,
// it forces a hard Stop().
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

// ---------------------------------------------------------------------------
// HMAC server-side auth interceptors
// ---------------------------------------------------------------------------

// DefaultAuthExemptMethods is the allow-list of gRPC methods that bypass
// HMACServerAuth (health probes, reflection).
var DefaultAuthExemptMethods = []string{
	"/grpc.health.v1.Health/Check",
	"/grpc.health.v1.Health/Watch",
	"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
}

// HMACServerAuthOptions tunes HMACServerAuth behavior.
type HMACServerAuthOptions struct {
	// ExtraExemptMethods is added to DefaultAuthExemptMethods.
	ExtraExemptMethods []string
}

// NewHMACServerAuth builds unary and stream gRPC interceptors that validate
// the "authorization: Bearer <token>" metadata header against the shared
// signingKey. On success the resolved Identity is threaded into the context.
func NewHMACServerAuth(signingKey string, opts *HMACServerAuthOptions) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor) {
	exempt := buildExemptSet(opts)

	authFn := func(ctx context.Context) (context.Context, error) {
		method, _ := grpc.Method(ctx)
		if _, bypass := exempt[method]; bypass {
			return ctx, nil
		}
		token, err := grpcauth.AuthFromMD(ctx, "bearer")
		if err != nil {
			return nil, err
		}
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "empty bearer token")
		}
		subject, object, verr := crawblhmac.ValidateToken(signingKey, token)
		if verr != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid bearer token")
		}
		return WithIdentity(ctx, Identity{Subject: subject, Object: object}), nil
	}

	return grpcauth.UnaryServerInterceptor(authFn), grpcauth.StreamServerInterceptor(authFn)
}

func buildExemptSet(opts *HMACServerAuthOptions) map[string]struct{} {
	total := len(DefaultAuthExemptMethods)
	if opts != nil {
		total += len(opts.ExtraExemptMethods)
	}
	set := make(map[string]struct{}, total)
	for _, m := range DefaultAuthExemptMethods {
		set[m] = struct{}{}
	}
	if opts != nil {
		for _, m := range opts.ExtraExemptMethods {
			set[strings.TrimSpace(m)] = struct{}{}
		}
	}
	return set
}
