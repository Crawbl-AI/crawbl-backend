package grpc

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
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

	unary := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, bypass := exempt[info.FullMethod]; bypass {
			return handler(ctx, req)
		}
		newCtx, err := authorizeContext(ctx, signingKey)
		if err != nil {
			return nil, err
		}
		return handler(newCtx, req)
	}
	stream := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if _, bypass := exempt[info.FullMethod]; bypass {
			return handler(srv, ss)
		}
		newCtx, err := authorizeContext(ss.Context(), signingKey)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: newCtx})
	}
	return unary, stream
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

func authorizeContext(ctx context.Context, signingKey string) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}
	token := strings.TrimSpace(vals[0])
	if idx := strings.IndexByte(token, ' '); idx >= 0 {
		scheme := strings.ToLower(strings.TrimSpace(token[:idx]))
		if scheme != "bearer" {
			return nil, status.Error(codes.Unauthenticated, "unsupported authorization scheme")
		}
		token = strings.TrimSpace(token[idx+1:])
	}
	if token == "" {
		return nil, status.Error(codes.Unauthenticated, "empty bearer token")
	}
	subject, object, err := crawblhmac.ValidateToken(signingKey, token)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid bearer token")
	}
	return WithIdentity(ctx, Identity{Subject: subject, Object: object}), nil
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }
