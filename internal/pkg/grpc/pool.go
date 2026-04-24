package grpc

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/grpc"
)

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
	if p == nil || p.dial == nil {
		return nil, errors.New("grpc: pool not initialized")
	}
	if p.closed.Load() {
		return nil, ErrPoolClosed
	}
	if target == "" {
		return nil, errors.New("grpc: empty target")
	}

	// Fast path: cached conn.
	if v, ok := p.conns.Load(target); ok {
		if conn, okCast := v.(*grpc.ClientConn); okCast && conn != nil {
			return conn, nil
		}
	}

	// Slow path: single-flight dial.
	v, err, _ := p.group.Do(target, func() (any, error) {
		return p.dialOnce(ctx, target)
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

// dialOnce is the singleflight body for Get. It double-checks the cache,
// dials if needed, and stores the connection atomically. Returns ErrPoolClosed
// if the pool was closed concurrently with the dial.
func (p *Pool) dialOnce(ctx context.Context, target string) (any, error) {
	// Double-check after acquiring the singleflight slot — another
	// goroutine may have finished dialing while we were waiting.
	if v, ok := p.conns.Load(target); ok {
		if conn, okCast := v.(*grpc.ClientConn); okCast && conn != nil {
			return conn, nil
		}
	}
	if p.closed.Load() {
		return nil, ErrPoolClosed
	}
	conn, dialErr := p.dial(ctx, target)
	if dialErr != nil {
		return nil, fmt.Errorf("grpc: dial %s: %w", target, dialErr)
	}
	// Store atomically. If Close ran in parallel, it may have
	// flipped `closed` between our check above and this Store;
	// defensively re-check and abort if so.
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
