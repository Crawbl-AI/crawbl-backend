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
	"fmt"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
)

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

// NewPool returns a Pool ready to Get connections via the provided
// DialFunc. The caller retains ownership of the DialFunc semantics
// (which credentials, TLS, keepalive params it installs) — the Pool
// itself is transport-agnostic.
func NewPool(dial DialFunc) *Pool {
	return &Pool{dial: dial}
}

// Get returns the cached *grpc.ClientConn for the given target,
// dialing via the DialFunc if the cache misses. The ctx bounds the
// dial step only — it does NOT bound the lifetime of the returned
// connection, which lives until Drop(target) or Close() is called.
//
// Concurrent Get calls for the same target share a single dial
// via singleflight: the first caller pays the dial cost, every
// subsequent caller waits and receives the same connection.
func (p *Pool) Get(ctx context.Context, target string) (*grpc.ClientConn, error) {
	if err := p.preflight(target); err != nil {
		return nil, err
	}

	if conn, ok := p.loadCachedConn(target); ok {
		return conn, nil
	}

	v, err, _ := p.group.Do(target, func() (any, error) {
		return p.singleFlightDial(ctx, target)
	})
	if err != nil {
		return nil, err
	}
	conn, ok := v.(*grpc.ClientConn)
	if !ok || conn == nil {
		return nil, errors.New("grpc: nil conn from dial")
	}
	return conn, nil
}

// preflight rejects calls made against an unusable pool: nil receiver,
// unset DialFunc, closed pool, or an empty target.
func (p *Pool) preflight(target string) error {
	if p == nil || p.dial == nil {
		return errors.New("grpc: pool not initialized")
	}
	if p.closed.Load() {
		return ErrPoolClosed
	}
	if target == "" {
		return errors.New("grpc: empty target")
	}
	return nil
}

// loadCachedConn returns the cached connection for target, or (nil, false)
// on cache miss / nil-entry so the caller falls through to the dial path.
func (p *Pool) loadCachedConn(target string) (*grpc.ClientConn, bool) {
	v, ok := p.conns.Load(target)
	if !ok {
		return nil, false
	}
	conn, okCast := v.(*grpc.ClientConn)
	if !okCast || conn == nil {
		return nil, false
	}
	return conn, true
}

// singleFlightDial is the slow-path body executed by singleflight.Group.Do.
// It double-checks the cache, dials a fresh connection, and handles the
// race where Close() fires between the pre-dial check and the Store.
func (p *Pool) singleFlightDial(ctx context.Context, target string) (any, error) {
	if conn, ok := p.loadCachedConn(target); ok {
		return conn, nil
	}
	if p.closed.Load() {
		return nil, ErrPoolClosed
	}
	conn, dialErr := p.dial(ctx, target)
	if dialErr != nil {
		return nil, fmt.Errorf("grpc: dial %s: %w", target, dialErr)
	}
	if p.closed.Load() {
		_ = conn.Close()
		return nil, ErrPoolClosed
	}
	p.conns.Store(target, conn)
	return conn, nil
}

// Drop evicts the cached connection for a target and closes it. Safe
// to call for a target that is not cached — returns without error.
//
// In-flight RPCs on the dropped connection will surface a transport
// error ("grpc: the client connection is closing") on their next Recv
// or Send. Callers that race Drop with active streams should retry
// via Get to re-dial.
func (p *Pool) Drop(target string) {
	if p == nil {
		return
	}
	if v, ok := p.conns.LoadAndDelete(target); ok {
		if conn, okCast := v.(*grpc.ClientConn); okCast && conn != nil {
			_ = conn.Close()
		}
	}
}

// Close tears down every cached connection and marks the pool as
// closed. Subsequent Get calls return ErrPoolClosed. Safe to call
// multiple times; subsequent calls are no-ops.
func (p *Pool) Close() error {
	if p == nil {
		return nil
	}
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	var firstErr error
	var mu sync.Mutex
	p.conns.Range(func(key, value any) bool {
		if conn, ok := value.(*grpc.ClientConn); ok && conn != nil {
			if err := conn.Close(); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}
		p.conns.Delete(key)
		return true
	})
	return firstErr
}
