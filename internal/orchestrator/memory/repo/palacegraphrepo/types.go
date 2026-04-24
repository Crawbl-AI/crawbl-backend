// Package palacegraphrepo provides palace graph navigation operations,
// including BFS traversal and tunnel detection across memory rooms.
package palacegraphrepo

import (
	"log/slog"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
)

// Consts.

// graphCacheTTL bounds how long a per-workspace room-node aggregation stays
// fresh in Redis before the next buildNodes call hits Postgres again.
const graphCacheTTL = 5 * time.Minute

// graphCacheKeyPrefix namespaces the palace-graph cache entries so they
// cannot collide with realtime-presence or rate-limit keys in the same
// Redis database.
const graphCacheKeyPrefix = "memory:palace:graph:"

const maxGraphResults = 50

// Types — structs.

// RoomNode is the internal graph-node shape built from memory_drawers
// groupings. It is cached per workspace inside graphCache and consumed by
// Traverse / FindTunnels / GraphStats to project domain results.
type RoomNode struct {
	Room  string   `json:"room"`
	Wings []string `json:"wings"`
	Halls []string `json:"halls"`
	Count int      `json:"count"`
}

// graphCache wraps a shared redisclient.Client with the (de)serialisation
// and error-logging bookkeeping the palace graph repo needs. When the
// injected redis client is nil the cache becomes a pass-through: get always
// misses and set is a no-op so Postgres still answers every query.
type graphCache struct {
	redis  redisclient.Client
	logger *slog.Logger
}

// Postgres is the palace-graph repository backed by PostgreSQL. It
// implements repo.PalaceGraphRepo; the per-workspace room-node aggregation
// is cached in Redis so repeated Traverse / FindTunnels / GraphStats calls
// reuse the same projection instead of re-scanning memory_drawers.
type Postgres struct {
	cache *graphCache
}

// frontier tracks a BFS cursor through the palace room graph.
type frontier struct {
	room  string
	depth int
}

// drawerMeta is a scan target for the aggregation query.
type drawerMeta struct {
	Room string `db:"room"`
	Wing string `db:"wing"`
	Hall string `db:"hall"`
	Cnt  int    `db:"cnt"`
}
