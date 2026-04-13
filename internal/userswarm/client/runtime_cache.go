package client

import (
	"context"
	"sync"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

// runtimeCacheTTL is how long a cached RuntimeStatus is considered fresh.
// 15 seconds balances API call reduction (~95%) with status freshness.
const runtimeCacheTTL = 15 * time.Second

// sweepInterval is how often the background goroutine prunes expired entries.
const sweepInterval = 60 * time.Second

// runtimeCache is a simple TTL-based cache mapping workspace IDs to their
// last-known RuntimeStatus. It is safe for concurrent use. A background
// sweep goroutine prunes expired entries to prevent unbounded growth.
type runtimeCache struct {
	mu      sync.RWMutex
	entries map[string]runtimeCacheEntry
}

type runtimeCacheEntry struct {
	status    *orchestrator.RuntimeStatus
	expiresAt time.Time
}

func newRuntimeCache(ctx context.Context) *runtimeCache {
	c := &runtimeCache{
		entries: make(map[string]runtimeCacheEntry),
	}
	go c.sweep(ctx)
	return c
}

// sweep periodically removes expired entries so the map doesn't grow
// unboundedly with stale workspace keys.
func (c *runtimeCache) sweep(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for k, e := range c.entries {
				if now.After(e.expiresAt) {
					delete(c.entries, k)
				}
			}
			c.mu.Unlock()
		}
	}
}

// get returns the cached RuntimeStatus if it exists and hasn't expired.
func (c *runtimeCache) get(workspaceID string) (*orchestrator.RuntimeStatus, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[workspaceID]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.status, true
}

// set stores a RuntimeStatus with a TTL. Only caches Verified=true statuses
// since non-verified statuses are transitional and should always be re-checked.
func (c *runtimeCache) set(workspaceID string, status *orchestrator.RuntimeStatus) {
	if status == nil || !status.Verified {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[workspaceID] = runtimeCacheEntry{
		status:    status,
		expiresAt: time.Now().Add(runtimeCacheTTL),
	}
}

// invalidate removes a workspace from the cache. Called on Delete or Update.
func (c *runtimeCache) invalidate(workspaceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, workspaceID)
}
