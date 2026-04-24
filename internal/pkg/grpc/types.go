// Package grpc holds shared gRPC infrastructure used by crawbl-backend
// components when they need to dial peer services (runtime pods, auth
// filters, future federation endpoints) or serve gRPC endpoints.
//
// The package is deliberately narrow and generic: it owns a connection
// pool, per-RPC HMAC credentials, standard keepalive parameters, and
// server-side interceptors. It has zero knowledge of UserSwarm,
// workspaces, memory, or any domain concept.
package grpc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

const (
	// DefaultDialTimeout caps the time a single gRPC dial attempt will
	// block before returning an error.
	DefaultDialTimeout = 5 * time.Second

	// DefaultCallTimeout bounds the duration of a non-streaming gRPC
	// call (e.g. Memory RPCs). Streaming calls use the request's own
	// context deadline.
	DefaultCallTimeout = 90 * time.Second

	// keepaliveIntervalSeconds is the idle ping interval in seconds.
	keepaliveIntervalSeconds = 30
)

// DefaultClientKeepalive is the keepalive parameters every crawbl-backend
// gRPC client should install via grpc.WithKeepaliveParams when dialing
// long-lived peer connections (like the per-workspace runtime pods).
var DefaultClientKeepalive = keepalive.ClientParameters{
	Time:                keepaliveIntervalSeconds * time.Second,
	Timeout:             10 * time.Second,
	PermitWithoutStream: true,
}

// ErrPoolClosed is returned by Get when the pool has been closed. It is
// a sentinel so callers can distinguish shutdown from transport errors
// and short-circuit retries.
var ErrPoolClosed = errors.New("grpc: pool is closed")

// DialFunc is the target-specific dial step the caller provides when
// constructing a Pool. It receives the target string chosen by the
// caller and returns a ready-to-use *grpc.ClientConn. The pool does
// not care which credentials, interceptors, or resolver the dial step
// installs — it only caches the resulting connection.
//
// Typical implementations install:
//   - transport credentials (TLS or insecure for cluster-internal)
//   - grpc.WithPerRPCCredentials(HMACCredentials{...}) for auth
//   - grpc.WithKeepaliveParams(DefaultClientKeepalive)
type DialFunc func(ctx context.Context, target string) (*grpc.ClientConn, error)

// Pool is a concurrency-safe, lazy cache of gRPC ClientConns keyed by
// target string.
//
// Design choices:
//
//   - Lazy dial. The first Get(target) invokes the DialFunc; subsequent
//     Get calls with the same target return the cached *grpc.ClientConn.
//     grpc.NewClient is non-blocking in grpc-go v1.80+, so the actual
//     network round-trip happens on the first RPC, not on Get.
//
//   - Single-flight dial. Concurrent first-access to a cold target is
//     coalesced via golang.org/x/sync/singleflight so only one DialFunc
//     call runs per target even under heavy orchestrator load. Without
//     this, the sync.Map LoadOrStore pattern would race and throw away
//     losing ClientConns.
//
//   - Drop on demand. Drop(target) evicts + closes a cached connection
//     so the next Get redials. Used when the caller knows a pod has
//     been recreated (workspace delete, pod reschedule, etc).
//
//   - Idempotent Close. Close flips an atomic closed flag and tears
//     down every cached connection. After Close, Get returns
//     ErrPoolClosed so late callers surface shutdown cleanly instead of
//     holding dead connections.
//
// The zero value is not usable — always construct via NewPool.
type Pool struct {
	dial   DialFunc
	conns  sync.Map // target(string) → *grpc.ClientConn
	group  singleflight.Group
	closed atomic.Bool
}

// Identity is the two-part (subject, object) pair carried on every
// authenticated gRPC RPC between crawbl-backend components. Subject is
// typically the user ID, Object is the workspace ID.
type Identity struct {
	Subject string
	Object  string
}

// identityKey is the private context key for Identity values.
type identityKey struct{}

// HMACCredentials is a grpc/credentials.PerRPCCredentials that signs every
// outbound RPC with an HMAC bearer token via internal/pkg/hmac.
type HMACCredentials struct {
	signingKey string
	requireTLS bool
}

var _ credentials.PerRPCCredentials = (*HMACCredentials)(nil)

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
