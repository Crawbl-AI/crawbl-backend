// Package grpc holds shared gRPC client-side infrastructure used by the
// orchestrator and other crawbl-backend components when they need to dial
// peer services (runtime pods, auth filters, future federation endpoints).
//
// The package is deliberately narrow and generic: it owns a connection
// pool, per-RPC HMAC credentials, and standard keepalive parameters.
// Every consumer builds its own DialFunc closure that installs exactly
// the transport credentials + interceptors it needs, then hands that
// closure to the Pool. The package has zero knowledge of UserSwarm,
// workspaces, memory, or any domain concept.
//
// Typical usage (from internal/userswarm/client):
//
//	dial := func(ctx context.Context, target string) (*grpc.ClientConn, error) {
//	    return grpc.NewClient(target,
//	        grpc.WithTransportCredentials(insecure.NewCredentials()),
//	        grpc.WithPerRPCCredentials(crawblgrpc.NewHMACCredentials(signingKey)),
//	        grpc.WithKeepaliveParams(crawblgrpc.DefaultClientKeepalive),
//	    )
//	}
//	pool := crawblgrpc.NewPool(dial)
//	defer pool.Close()
//
//	conn, err := pool.Get(ctx, "workspace-abc.userswarms.svc.cluster.local:42618")
//	// conn.NewClientStream(...) -- RPCs automatically carry the HMAC
//	// bearer token derived from ctx via crawblgrpc.WithAuthIdentity().
package grpc
