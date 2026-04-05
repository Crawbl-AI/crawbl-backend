// Package server wires the gRPC runtime server for crawbl-agent-runtime.
// This file implements the HMAC-bearer authentication interceptor applied
// to every Converse and Memory RPC.
package server

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

// authCtxKey is the private context key under which the validated principal
// is stashed after a successful auth check. Downstream handlers read it via
// PrincipalFromContext.
type authCtxKey struct{}

// Principal is the identity resolved from a validated HMAC bearer token.
// Parts map to the orchestrator's HMAC scheme: UserID + WorkspaceID.
type Principal struct {
	UserID      string
	WorkspaceID string
}

// PrincipalFromContext retrieves the authenticated principal set by the
// HMAC interceptor. Returns ok=false when no principal is present (i.e.
// the RPC was called without the interceptor chain, which should only
// happen in tests).
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(authCtxKey{}).(Principal)
	return p, ok
}

// withPrincipal returns a copy of ctx carrying the principal.
func withPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, authCtxKey{}, p)
}

// exemptMethods is the allow-list of unauthenticated gRPC methods. k8s
// readiness / liveness probes hit grpc.health.v1.Health without any
// credentials (kubelet cannot sign HMAC tokens), so those methods MUST
// bypass the auth chain. The grpc.reflection service is also listed
// because local debugging via grpcurl --reflection relies on it.
var exemptMethods = map[string]struct{}{
	"/grpc.health.v1.Health/Check":                  {},
	"/grpc.health.v1.Health/Watch":                  {},
	"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo":      {},
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo": {},
}

// HMACAuth builds unary and stream gRPC interceptors that validate the
// "authorization: Bearer <token>" metadata header against the shared
// signingKey using internal/pkg/hmac. On success the resolved Principal is
// threaded into the request context.
//
// This is the runtime's server-side mirror of the token-generation path used
// by cmd/crawbl/platform/orchestrator/orchestrator.go:269 and
// internal/orchestrator/mcp/server.go:66, which share the same HMAC scheme.
//
// Methods in exemptMethods (notably grpc.health.v1.Health) bypass auth so
// kubelet probes and local grpcurl reflection keep working.
func HMACAuth(signingKey string) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor) {
	unary := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, exempt := exemptMethods[info.FullMethod]; exempt {
			return handler(ctx, req)
		}
		newCtx, err := authorizeContext(ctx, signingKey)
		if err != nil {
			return nil, err
		}
		return handler(newCtx, req)
	}
	stream := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if _, exempt := exemptMethods[info.FullMethod]; exempt {
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

// authorizeContext inspects the incoming metadata, validates the bearer
// token, and returns a ctx carrying the Principal on success.
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
	userID, workspaceID, err := crawblhmac.ValidateToken(signingKey, token)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid bearer token")
	}
	return withPrincipal(ctx, Principal{UserID: userID, WorkspaceID: workspaceID}), nil
}

// wrappedStream replaces grpc.ServerStream.Context() with a context that
// carries the validated Principal. gRPC does not allow mutating the
// underlying stream's context directly, so we shadow it.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }
