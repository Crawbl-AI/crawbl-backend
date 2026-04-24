package palacegraphrepo

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
)

// newGraphCache wires a graphCache around the shared redis client. A nil
// client is valid (cache disabled); a nil logger falls back to slog.Default.
func newGraphCache(redis redisclient.Client, logger *slog.Logger) *graphCache {
	if logger == nil {
		logger = slog.Default()
	}
	return &graphCache{redis: redis, logger: logger}
}

// get returns the cached room-node map for a workspace. Any cache-layer
// error is logged and reported as a miss so callers always fall through to
// the authoritative DB read rather than surfacing Redis flakes to users.
func (c *graphCache) get(ctx context.Context, workspaceID string) (map[string]*RoomNode, bool) {
	if c.redis == nil {
		return nil, false
	}
	raw, err := c.redis.Get(ctx, graphCacheKey(workspaceID))
	if err != nil {
		c.logger.WarnContext(ctx, "palacegraphrepo: cache get failed",
			slog.String("workspace_id", workspaceID),
			slog.String("error", err.Error()),
		)
		return nil, false
	}
	if raw == "" {
		return nil, false
	}
	var nodes map[string]*RoomNode
	if err := json.Unmarshal([]byte(raw), &nodes); err != nil {
		c.logger.WarnContext(ctx, "palacegraphrepo: cache decode failed",
			slog.String("workspace_id", workspaceID),
			slog.String("error", err.Error()),
		)
		return nil, false
	}
	return nodes, true
}

// set stores the aggregated room-node map for a workspace. Encoding or
// Redis write errors are logged and swallowed — the next live read will
// simply rebuild the nodes and try again.
func (c *graphCache) set(ctx context.Context, workspaceID string, nodes map[string]*RoomNode) {
	if c.redis == nil {
		return
	}
	payload, err := json.Marshal(nodes)
	if err != nil {
		c.logger.WarnContext(ctx, "palacegraphrepo: cache encode failed",
			slog.String("workspace_id", workspaceID),
			slog.String("error", err.Error()),
		)
		return
	}
	if err := c.redis.Set(ctx, graphCacheKey(workspaceID), string(payload), graphCacheTTL); err != nil {
		c.logger.WarnContext(ctx, "palacegraphrepo: cache set failed",
			slog.String("workspace_id", workspaceID),
			slog.String("error", err.Error()),
		)
	}
}

// invalidate clears the cached aggregation for a workspace. Unused by the
// current read path (TTL eviction is enough) but exposed for future
// write-through hooks and test harnesses.
func (c *graphCache) invalidate(ctx context.Context, workspaceID string) {
	if c.redis == nil {
		return
	}
	if err := c.redis.Del(ctx, graphCacheKey(workspaceID)); err != nil {
		c.logger.WarnContext(ctx, "palacegraphrepo: cache del failed",
			slog.String("workspace_id", workspaceID),
			slog.String("error", err.Error()),
		)
	}
}

// graphCacheKey namespaces the workspace's aggregation blob.
func graphCacheKey(workspaceID string) string {
	return graphCacheKeyPrefix + workspaceID
}
