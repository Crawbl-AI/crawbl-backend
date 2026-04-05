package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/credentials"

	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

// Identity is the two-part (subject, object) pair carried on every
// authenticated gRPC RPC between crawbl-backend components. It maps
// directly onto internal/pkg/hmac.GenerateToken's two identity
// arguments: Subject goes into position 1, Object into position 2.
//
// In practice Subject is the platform user ID and Object is the
// workspace ID — but the gRPC infrastructure layer treats them as
// opaque strings so the same type can be reused by any future
// service-to-service call that needs a signed identity pair.
//
// The type is exported so both client-side credentials and server-side
// interceptors share it; the server's handler reads the validated
// Identity from context via IdentityFromContext.
type Identity struct {
	Subject string
	Object  string
}

// identityKey is the private context key under which both client-side
// WithIdentity and server-side HMACServerAuth store the Identity value.
// Using a typed key (unexported struct) prevents collisions with any
// other context key in the codebase.
type identityKey struct{}

// WithIdentity stamps an Identity onto the context. Client-side callers
// invoke this BEFORE opening a gRPC stream so that the HMACCredentials
// installed on the ClientConn can read the identity off the request
// ctx and sign the bearer header automatically.
//
// Typical use:
//
//	ctx = crawblgrpc.WithIdentity(ctx, crawblgrpc.Identity{
//	    Subject: userID,
//	    Object:  workspaceID,
//	})
//	stream, err := client.Converse(ctx)
//
// If no identity is stamped, HMACCredentials.GetRequestMetadata returns
// an error that surfaces as codes.Unauthenticated on the server side —
// fail-closed by design.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFromContext extracts the Identity stamped by WithIdentity
// (client-side) or HMACServerAuth (server-side). Returns (zero, false)
// when no identity is present, which happens in two cases:
//
//  1. Client-side code forgot to call WithIdentity before dialing —
//     the HMACCredentials path fails the RPC before the handler runs.
//  2. Server-side code called a handler without the HMACServerAuth
//     interceptor installed — typically a test path; production
//     servers always chain the interceptor.
//
// Handlers that require authentication should treat `ok=false` as a
// programming error and return codes.Internal or panic.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	v, ok := ctx.Value(identityKey{}).(Identity)
	return v, ok
}

// HMACCredentials is a google.golang.org/grpc/credentials.PerRPCCredentials
// implementation that signs every outbound RPC with an HMAC bearer
// token generated via internal/pkg/hmac.GenerateToken.
//
// It is installed on a grpc.ClientConn once at dial time via
// grpc.WithPerRPCCredentials(creds). Every unary + stream RPC on that
// connection then carries an Authorization: Bearer <token> metadata
// header automatically — callers do NOT need to attach it manually.
//
// The Identity that goes into the token is read from the request
// context via WithIdentity, so a single connection can safely serve
// RPCs for multiple users / workspaces without rebuilding credentials
// per call. If no identity is on the context, GetRequestMetadata
// returns an error and the RPC fails closed.
type HMACCredentials struct {
	signingKey string
	requireTLS bool
}

// NewHMACCredentials builds the credentials with the given shared key.
// requireTLS=false because the runtime connection is cluster-internal;
// set it to true via WithRequireTransportSecurity if the pool ever
// dials across an untrusted network.
func NewHMACCredentials(signingKey string) *HMACCredentials {
	return &HMACCredentials{signingKey: signingKey}
}

// WithRequireTransportSecurity returns a copy of the credentials that
// refuses to run over insecure transports. Use this for any future
// cross-cluster or internet-facing gRPC client.
func (c *HMACCredentials) WithRequireTransportSecurity() *HMACCredentials {
	if c == nil {
		return nil
	}
	return &HMACCredentials{signingKey: c.signingKey, requireTLS: true}
}

// GetRequestMetadata implements credentials.PerRPCCredentials. It reads
// the Identity stamped on the context and returns the bearer header.
func (c *HMACCredentials) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	if c == nil || c.signingKey == "" {
		return nil, errors.New("grpc: HMACCredentials: missing signing key")
	}
	id, ok := IdentityFromContext(ctx)
	if !ok || id.Subject == "" || id.Object == "" {
		return nil, errors.New("grpc: HMACCredentials: missing identity on context (call crawblgrpc.WithIdentity)")
	}
	token := crawblhmac.GenerateToken(c.signingKey, id.Subject, id.Object)
	return map[string]string{
		"authorization": "Bearer " + token,
	}, nil
}

// RequireTransportSecurity implements credentials.PerRPCCredentials.
func (c *HMACCredentials) RequireTransportSecurity() bool {
	if c == nil {
		return false
	}
	return c.requireTLS
}

// Static assertion that HMACCredentials implements the interface.
var _ credentials.PerRPCCredentials = (*HMACCredentials)(nil)
