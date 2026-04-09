package graph

import (
	"sync"
	"time"
)

const graphCacheTTL = 5 * time.Minute

type cacheEntry struct {
	nodes     map[string]*RoomNode
	createdAt time.Time
}

type graphCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry // keyed by workspaceID
}

func newGraphCache() *graphCache {
	return &graphCache{
		entries: make(map[string]*cacheEntry),
	}
}

func (c *graphCache) get(workspaceID string) (map[string]*RoomNode, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[workspaceID]
	if !ok || time.Since(entry.createdAt) > graphCacheTTL {
		return nil, false
	}
	return entry.nodes, true
}

func (c *graphCache) set(workspaceID string, nodes map[string]*RoomNode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[workspaceID] = &cacheEntry{
		nodes:     nodes,
		createdAt: time.Now(),
	}
}

func (c *graphCache) invalidate(workspaceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, workspaceID)
}
