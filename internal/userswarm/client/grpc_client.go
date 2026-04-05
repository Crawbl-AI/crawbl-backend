package client

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	crawblgrpc "github.com/Crawbl-AI/crawbl-backend/internal/pkg/grpc"
)

// newGRPCPool constructs the *crawblgrpc.Pool the production client
// uses to cache per-workspace gRPC connections. The dial closure wired
// in here installs:
//
//   - insecure transport credentials (cluster-internal traffic, TLS is
//     unnecessary and would add cert management overhead for the
//     per-workspace service DNS names)
//   - HMAC per-RPC credentials that sign every call with a bearer
//     derived from (userID, workspaceID) read from the request context
//     via crawblgrpc.WithAuthIdentity
//   - standard client keepalive parameters so idle connections
//     survive NAT timeouts and dead pods surface fast
//
// The pool lives for the lifetime of the *userSwarmClient; Close tears
// down every cached connection.
func newGRPCPool(signingKey string) *crawblgrpc.Pool {
	creds := crawblgrpc.NewHMACCredentials(signingKey)

	dial := func(_ context.Context, target string) (*grpc.ClientConn, error) {
		// grpc.NewClient is non-blocking in grpc-go v1.80+: the TCP
		// connection is established lazily on the first RPC. We don't
		// use DialContext + WithBlock because the orchestrator prefers
		// fail-fast-per-RPC over fail-fast-at-dial: a momentarily
		// unreachable pod should surface as a per-request error, not a
		// permanent cache poison.
		return grpc.NewClient(
			target,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithPerRPCCredentials(creds),
			grpc.WithKeepaliveParams(crawblgrpc.DefaultClientKeepalive),
		)
	}

	return crawblgrpc.NewPool(dial)
}

// grpcTarget builds the in-cluster DNS name the pool uses as a cache
// key for a workspace runtime pod. Format:
//
//	<service>.<namespace>.svc.cluster.local:<port>
//
// service + namespace come from the UserSwarm status (populated by the
// webhook). The port is c.config.Port, which defaults to
// DefaultRuntimePort (42618).
func (c *userSwarmClient) grpcTarget(serviceName, namespace string) (string, error) {
	service := strings.TrimSpace(serviceName)
	ns := strings.TrimSpace(namespace)
	if service == "" || ns == "" {
		return "", fmt.Errorf("grpc: runtime missing service (%q) or namespace (%q)", service, ns)
	}
	port := c.config.Port
	if port <= 0 {
		port = DefaultRuntimePort
	}
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", service, ns, port), nil
}

// conn returns the cached gRPC connection for the given runtime, or
// dials via the pool on cache miss. The returned *grpc.ClientConn
// carries HMAC per-RPC credentials pre-installed at dial time — callers
// do NOT need to stamp Authorization metadata manually. They DO need
// to carry an auth identity on the request ctx via
// crawblgrpc.WithAuthIdentity(ctx, userID, workspaceID) before invoking
// any RPC, otherwise the credentials will fail the call with a clear
// error.
func (c *userSwarmClient) conn(ctx context.Context, runtime *runtimeCoord) (*grpc.ClientConn, error) {
	if runtime == nil {
		return nil, errors.New("grpc: nil runtime coordinates")
	}
	target, err := c.grpcTarget(runtime.serviceName, runtime.namespace)
	if err != nil {
		return nil, err
	}
	return c.grpcPool.Get(ctx, target)
}

// runtimeCoord is the minimal set of fields the gRPC dial path needs
// from an orchestrator.RuntimeStatus. Extracting it as a private type
// keeps grpc_converse.go and grpc_memory.go from having to import
// orchestrator at the low level where the pool call happens — they
// already do at the outer level, but this tighter contract documents
// exactly which fields matter for dialing.
type runtimeCoord struct {
	serviceName string
	namespace   string
}
