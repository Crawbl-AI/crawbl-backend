package grpc

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

// DefaultAuthExemptMethods is the allow-list of gRPC methods that
// bypass HMACServerAuth. kubelet health probes and grpcurl reflection
// requests cannot (and should not) carry HMAC bearer tokens — the
// server still needs to answer them for cluster liveness + local
// debugging.
//
// Callers that need to extend the list pass their own set to
// NewHMACServerAuth; this slice is the baseline and is included
// automatically unless the caller replaces it entirely.
var DefaultAuthExemptMethods = []string{
	"/grpc.health.v1.Health/Check",
	"/grpc.health.v1.Health/Watch",
	"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
}

// HMACServerAuthOptions tunes HMACServerAuth behavior. Zero values
// are safe defaults.
type HMACServerAuthOptions struct {
	// ExtraExemptMethods is added to DefaultAuthExemptMethods to form
	// the final exempt set. Use this when a service needs to expose an
	// unauthenticated method (e.g. a public metadata endpoint) without
	// dropping the baseline health/reflection bypass.
	ExtraExemptMethods []string
}

// NewHMACServerAuth builds unary and stream gRPC interceptors that
// validate the "authorization: Bearer <token>" metadata header against
// the shared signingKey using internal/pkg/hmac. On success the
// resolved Identity is threaded into the request context where
// handlers read it via IdentityFromContext.
//
// This is the server-side mirror of HMACCredentials. Install on the
// grpc.Server via:
//
//	unary, stream := crawblgrpc.NewHMACServerAuth(signingKey, nil)
//	srv := grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(unary),
//	    grpc.ChainStreamInterceptor(stream),
//	)
//
// Methods in DefaultAuthExemptMethods (plus any extras in opts) bypass
// auth so kubelet probes and local grpcurl reflection keep working.
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

// buildExemptSet combines DefaultAuthExemptMethods with any extras the
// caller provided. Returns a map for O(1) lookup in the hot path.
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

// authorizeContext inspects incoming metadata, validates the bearer
// token, and returns a ctx carrying the resolved Identity.
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
	// Accept "Bearer <token>" (case-insensitive prefix) or a bare token.
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

// wrappedStream replaces grpc.ServerStream.Context() with a context
// that carries the validated Identity. gRPC does not allow mutating
// the underlying stream's context directly, so we shadow it.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }
