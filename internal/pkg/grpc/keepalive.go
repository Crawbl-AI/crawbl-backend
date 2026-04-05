package grpc

import (
	"time"

	"google.golang.org/grpc/keepalive"
)

// DefaultClientKeepalive is the keepalive parameters every crawbl-backend
// gRPC client should install via grpc.WithKeepaliveParams when dialing
// long-lived peer connections (like the per-workspace runtime pods).
//
// Why these values:
//
//   - Time: 30s — ping the peer every 30 seconds when idle. This keeps
//     NAT table entries alive on intermediate network devices and
//     surfaces half-open TCP sockets quickly (dead pods that the
//     cluster hasn't yet re-announced).
//
//   - Timeout: 10s — wait at most 10 seconds for a keepalive pong
//     before marking the connection as dead. Short enough that a
//     rescheduled pod produces a visible error, long enough to survive
//     transient network hiccups.
//
//   - PermitWithoutStream: true — send keepalives even when no RPC is
//     in flight. Required because the orchestrator may hold a cached
//     connection idle for minutes between user messages, and without
//     this flag grpc-go would let the connection go silent until the
//     next RPC discovers it's broken.
//
// Matching server-side parameters live on the runtime's grpc.Server;
// if the server ever tightens its grpc/keepalive.EnforcementPolicy, the
// client values here must stay within the allowed range to avoid
// "too many pings" termination.
var DefaultClientKeepalive = keepalive.ClientParameters{
	Time:                30 * time.Second,
	Timeout:             10 * time.Second,
	PermitWithoutStream: true,
}
