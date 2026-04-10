package palacegraphrepo

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
)

const maxGraphResults = 50

// frontier tracks a BFS cursor through the palace room graph.
type frontier struct {
	room  string
	depth int
}

// Postgres is the palace-graph repository backed by PostgreSQL. It
// implements repo.PalaceGraphRepo; the per-workspace room-node aggregation
// is cached in Redis so repeated Traverse / FindTunnels / GraphStats calls
// reuse the same projection instead of re-scanning memory_drawers.
type Postgres struct {
	cache *graphCache
}

// NewPostgres returns a palace-graph repository backed by Postgres. Pass a
// non-nil redisclient.Client to enable the shared TTL cache; nil disables
// caching entirely and every call hits Postgres directly.
func NewPostgres(redis redisclient.Client, logger *slog.Logger) *Postgres {
	return &Postgres{
		cache: newGraphCache(redis, logger),
	}
}

// drawerMeta is a scan target for the aggregation query.
type drawerMeta struct {
	Room string `db:"room"`
	Wing string `db:"wing"`
	Hall string `db:"hall"`
	Cnt  int    `db:"cnt"`
}

func (g *Postgres) buildNodes(ctx context.Context, sess database.SessionRunner, workspaceID string) (map[string]*RoomNode, error) {
	if cached, ok := g.cache.get(ctx, workspaceID); ok {
		return cached, nil
	}

	var rows []drawerMeta
	_, err := sess.Select("room", "wing", "hall", "COUNT(*) AS cnt").
		From("memory_drawers").
		Where("workspace_id = ?", workspaceID).
		Where("room != ''").
		Where("room != 'general'").
		GroupBy("room", "wing", "hall").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("graph: build nodes: %w", err)
	}

	nodes := make(map[string]*RoomNode)
	for _, r := range rows {
		node, ok := nodes[r.Room]
		if !ok {
			node = &RoomNode{Room: r.Room}
			nodes[r.Room] = node
		}
		if !containsStr(node.Wings, r.Wing) {
			node.Wings = append(node.Wings, r.Wing)
		}
		if r.Hall != "" && !containsStr(node.Halls, r.Hall) {
			node.Halls = append(node.Halls, r.Hall)
		}
		node.Count += r.Cnt
	}

	for _, node := range nodes {
		sort.Strings(node.Wings)
		sort.Strings(node.Halls)
	}

	g.cache.set(ctx, workspaceID, nodes)
	return nodes, nil
}

// Traverse walks the graph from startRoom via BFS up to maxHops.
func (g *Postgres) Traverse(ctx context.Context, sess database.SessionRunner, workspaceID, startRoom string, maxHops int) ([]memory.TraversalResult, error) {
	if maxHops <= 0 {
		maxHops = 2
	}
	nodes, err := g.buildNodes(ctx, sess, workspaceID)
	if err != nil {
		return nil, err
	}

	start, ok := nodes[startRoom]
	if !ok {
		return nil, fmt.Errorf("graph: room %q not found", startRoom)
	}

	visited := map[string]bool{startRoom: true}
	results := []memory.TraversalResult{{
		Room:  startRoom,
		Wings: start.Wings,
		Halls: start.Halls,
		Count: start.Count,
		Hop:   0,
	}}

	queue := []frontier{{startRoom, 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= maxHops {
			continue
		}
		queue = expandFrontier(nodes, visited, &results, queue, cur, maxHops)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Hop != results[j].Hop {
			return results[i].Hop < results[j].Hop
		}
		return results[i].Count > results[j].Count
	})

	if len(results) > maxGraphResults {
		results = results[:maxGraphResults]
	}
	return results, nil
}

// FindTunnels returns rooms that appear in both wingA and wingB (or any 2+ wings if both empty).
func (g *Postgres) FindTunnels(ctx context.Context, sess database.SessionRunner, workspaceID, wingA, wingB string) ([]memory.Tunnel, error) {
	nodes, err := g.buildNodes(ctx, sess, workspaceID)
	if err != nil {
		return nil, err
	}

	var tunnels []memory.Tunnel
	for _, node := range nodes {
		if len(node.Wings) < 2 {
			continue
		}
		if wingA != "" && !containsStr(node.Wings, wingA) {
			continue
		}
		if wingB != "" && !containsStr(node.Wings, wingB) {
			continue
		}
		tunnels = append(tunnels, memory.Tunnel{
			Room:  node.Room,
			Wings: node.Wings,
			Halls: node.Halls,
			Count: node.Count,
		})
	}

	sort.Slice(tunnels, func(i, j int) bool { return tunnels[i].Count > tunnels[j].Count })
	if len(tunnels) > maxGraphResults {
		tunnels = tunnels[:maxGraphResults]
	}
	return tunnels, nil
}

// GraphStats returns a summary of the palace graph structure.
func (g *Postgres) GraphStats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.PalaceGraphStats, error) {
	nodes, err := g.buildNodes(ctx, sess, workspaceID)
	if err != nil {
		return nil, err
	}

	tunnelCount := 0
	wingCounts := make(map[string]int)
	totalEdges := 0
	var topTunnels []memory.Tunnel

	for _, node := range nodes {
		addWingCounts(wingCounts, node.Wings)
		totalEdges += wingEdges(len(node.Wings))
		if len(node.Wings) < 2 {
			continue
		}
		tunnelCount++
		topTunnels = append(topTunnels, memory.Tunnel{
			Room:  node.Room,
			Wings: node.Wings,
			Halls: node.Halls,
			Count: node.Count,
		})
	}

	sort.Slice(topTunnels, func(i, j int) bool {
		return len(topTunnels[i].Wings) > len(topTunnels[j].Wings)
	})
	if len(topTunnels) > 10 {
		topTunnels = topTunnels[:10]
	}

	return &memory.PalaceGraphStats{
		TotalRooms:   len(nodes),
		TunnelRooms:  tunnelCount,
		TotalEdges:   totalEdges,
		RoomsPerWing: wingCounts,
		TopTunnels:   topTunnels,
	}, nil
}

// expandFrontier visits every unseen room that shares a wing with cur,
// appends traversal results for each one, and returns the queue extended
// with any rooms still within the hop budget.
func expandFrontier(
	nodes map[string]*RoomNode,
	visited map[string]bool,
	results *[]memory.TraversalResult,
	queue []frontier,
	cur frontier,
	maxHops int,
) []frontier {
	curNode := nodes[cur.room]
	curWings := toSet(curNode.Wings)
	for room, node := range nodes {
		if visited[room] {
			continue
		}
		shared := intersect(curWings, toSet(node.Wings))
		if len(shared) == 0 {
			continue
		}
		visited[room] = true
		*results = append(*results, memory.TraversalResult{
			Room:         room,
			Wings:        node.Wings,
			Halls:        node.Halls,
			Count:        node.Count,
			Hop:          cur.depth + 1,
			ConnectedVia: setToSorted(shared),
		})
		if cur.depth+1 < maxHops {
			queue = append(queue, frontier{room, cur.depth + 1})
		}
	}
	return queue
}

// addWingCounts increments per-wing counters for one room.
func addWingCounts(counts map[string]int, wings []string) {
	for _, w := range wings {
		counts[w]++
	}
}

// wingEdges returns the edge contribution of a single room to the palace
// graph: the number of wing pairs that share this room. Zero when the
// room belongs to fewer than two wings.
func wingEdges(wingCount int) int {
	if wingCount < 2 {
		return 0
	}
	return wingCount * (wingCount - 1) / 2
}

// helpers

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func intersect(a, b map[string]bool) map[string]bool {
	result := make(map[string]bool)
	for k := range a {
		if b[k] {
			result[k] = true
		}
	}
	return result
}

func setToSorted(m map[string]bool) []string {
	ss := make([]string, 0, len(m))
	for k := range m {
		ss = append(ss, k)
	}
	sort.Strings(ss)
	return ss
}
