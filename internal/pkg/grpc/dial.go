package grpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// NewInsecureHMACPool creates a connection pool with insecure transport,
// HMAC per-RPC credentials, and default keepalive. This is the standard
// pool configuration for cluster-internal gRPC traffic where TLS is
// unnecessary and would add cert management overhead.
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
