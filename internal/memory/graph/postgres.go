package graph

import (
	"context"
	"fmt"
	"sort"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

const maxGraphResults = 50

type postgresGraph struct {
	cache *graphCache
}

// NewPostgres returns a PalaceGraph backed by Postgres.
func NewPostgres() PalaceGraph {
	return &postgresGraph{
		cache: newGraphCache(),
	}
}

// drawerMeta is a scan target for the aggregation query.
type drawerMeta struct {
	Room string `db:"room"`
	Wing string `db:"wing"`
	Hall string `db:"hall"`
	Cnt  int    `db:"cnt"`
}

func (g *postgresGraph) buildNodes(ctx context.Context, sess database.SessionRunner, workspaceID string) (map[string]*RoomNode, error) {
	if cached, ok := g.cache.get(workspaceID); ok {
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

	g.cache.set(workspaceID, nodes)
	return nodes, nil
}

// Traverse walks the graph from startRoom via BFS up to maxHops.
func (g *postgresGraph) Traverse(ctx context.Context, sess database.SessionRunner, workspaceID, startRoom string, maxHops int) ([]TraversalResult, error) {
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
	results := []TraversalResult{{
		Room:  startRoom,
		Wings: start.Wings,
		Halls: start.Halls,
		Count: start.Count,
		Hop:   0,
	}}

	type frontier struct {
		room  string
		depth int
	}
	queue := []frontier{{startRoom, 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= maxHops {
			continue
		}

		curNode := nodes[cur.room]
		curWings := toSet(curNode.Wings)

		for room, node := range nodes {
			if visited[room] {
				continue
			}
			shared := intersect(curWings, toSet(node.Wings))
			if len(shared) > 0 {
				visited[room] = true
				results = append(results, TraversalResult{
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
		}
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
func (g *postgresGraph) FindTunnels(ctx context.Context, sess database.SessionRunner, workspaceID, wingA, wingB string) ([]Tunnel, error) {
	nodes, err := g.buildNodes(ctx, sess, workspaceID)
	if err != nil {
		return nil, err
	}

	var tunnels []Tunnel
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
		tunnels = append(tunnels, Tunnel{
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
func (g *postgresGraph) GraphStats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*Stats, error) {
	nodes, err := g.buildNodes(ctx, sess, workspaceID)
	if err != nil {
		return nil, err
	}

	tunnelCount := 0
	wingCounts := make(map[string]int)
	var topTunnels []Tunnel

	for _, node := range nodes {
		for _, w := range node.Wings {
			wingCounts[w]++
		}
		if len(node.Wings) >= 2 {
			tunnelCount++
			topTunnels = append(topTunnels, Tunnel{
				Room:  node.Room,
				Wings: node.Wings,
				Halls: node.Halls,
				Count: node.Count,
			})
		}
	}

	sort.Slice(topTunnels, func(i, j int) bool {
		return len(topTunnels[i].Wings) > len(topTunnels[j].Wings)
	})
	if len(topTunnels) > 10 {
		topTunnels = topTunnels[:10]
	}

	totalEdges := 0
	for _, node := range nodes {
		n := len(node.Wings)
		if n >= 2 {
			totalEdges += n * (n - 1) / 2
		}
	}

	return &Stats{
		TotalRooms:   len(nodes),
		TunnelRooms:  tunnelCount,
		TotalEdges:   totalEdges,
		RoomsPerWing: wingCounts,
		TopTunnels:   topTunnels,
	}, nil
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
