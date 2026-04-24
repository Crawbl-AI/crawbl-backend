package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

// ClusterTarget builds a Kubernetes in-cluster DNS target for gRPC dialing.
// Format: "<service>.<namespace>.svc.cluster.local:<port>"
func ClusterTarget(service, namespace string, port int32) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", service, namespace, port)
}

// DirectTarget formats an IP:port pair suitable for gRPC dialing. It wraps
// IPv6 addresses in brackets per RFC 3986 so grpc.NewClient parses them
// correctly (e.g. "[::1]:42618" vs "10.0.0.1:42618").
func DirectTarget(ip string, port int32) string {
	if strings.ContainsRune(ip, ':') {
		return fmt.Sprintf("[%s]:%d", ip, port)
	}
	return fmt.Sprintf("%s:%d", ip, port)
}

// NewInsecureHMACPool creates a connection pool with insecure transport,
// HMAC per-RPC credentials, and default keepalive. This is the standard
// pool configuration for cluster-internal gRPC traffic.
func NewInsecureHMACPool(signingKey string) *Pool {
	creds := NewHMACCredentials(signingKey)

	dial := func(_ context.Context, target string) (*grpc.ClientConn, error) {
		return grpc.NewClient(
			target,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithPerRPCCredentials(creds),
			grpc.WithKeepaliveParams(DefaultClientKeepalive),
		)
	}

	return NewPool(dial)
}

// NewTLSHMACPool creates a connection pool with mTLS transport, HMAC
// per-RPC credentials, and default keepalive. This is the pool
// configuration for cross-cluster gRPC traffic in prod hybrid mode
// where the runtime cluster is on a separate network.
func NewTLSHMACPool(signingKey string, tlsCfg *tls.Config) *Pool {
	creds := NewHMACCredentials(signingKey).WithRequireTransportSecurity()

	dial := func(_ context.Context, target string) (*grpc.ClientConn, error) {
		return grpc.NewClient(
			target,
			grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
			grpc.WithPerRPCCredentials(creds),
			grpc.WithKeepaliveParams(DefaultClientKeepalive),
		)
	}

	return NewPool(dial)
}

// WithIdentity stamps an Identity onto the context so that HMACCredentials
// can sign the bearer header automatically.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFromContext extracts the Identity stamped by WithIdentity
// (client-side) or HMACServerAuth (server-side).
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	v, ok := ctx.Value(identityKey{}).(Identity)
	return v, ok
}

// NewHMACCredentials builds credentials with the given shared key.
func NewHMACCredentials(signingKey string) *HMACCredentials {
	return &HMACCredentials{signingKey: signingKey}
}

// WithRequireTransportSecurity returns a copy that refuses insecure transports.
func (c *HMACCredentials) WithRequireTransportSecurity() *HMACCredentials {
	if c == nil {
		return nil
	}
	return &HMACCredentials{signingKey: c.signingKey, requireTLS: true}
}

// GetRequestMetadata implements credentials.PerRPCCredentials.
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

// FormatProtoTimestamp renders a google.protobuf.Timestamp to an RFC3339
// UTC string. Returns "" for nil or zero timestamps.
func FormatProtoTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	t := ts.AsTime()
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
