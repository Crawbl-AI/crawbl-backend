// Package graph provides palace graph navigation operations,
// including BFS traversal and tunnel detection across memory rooms.
package graph

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// RoomNode represents a room in the palace graph.
type RoomNode struct {
	Room  string   `json:"room"`
	Wings []string `json:"wings"`
	Halls []string `json:"halls"`
	Count int      `json:"count"`
}

// TraversalResult is a room found during BFS traversal.
type TraversalResult struct {
	Room         string   `json:"room"`
	Wings        []string `json:"wings"`
	Halls        []string `json:"halls"`
	Count        int      `json:"count"`
	Hop          int      `json:"hop"`
	ConnectedVia []string `json:"connected_via,omitempty"`
}

// Tunnel is a room that bridges two or more wings.
type Tunnel struct {
	Room  string   `json:"room"`
	Wings []string `json:"wings"`
	Halls []string `json:"halls"`
	Count int      `json:"count"`
}

// Stats holds palace graph statistics.
type Stats struct {
	TotalRooms   int            `json:"total_rooms"`
	TunnelRooms  int            `json:"tunnel_rooms"`
	TotalEdges   int            `json:"total_edges"`
	RoomsPerWing map[string]int `json:"rooms_per_wing"`
	TopTunnels   []Tunnel       `json:"top_tunnels"`
}

// PalaceGraph provides palace navigation operations.
type PalaceGraph interface {
	// Traverse walks the graph from a starting room via BFS.
	Traverse(ctx context.Context, sess database.SessionRunner, workspaceID, startRoom string, maxHops int) ([]TraversalResult, error)

	// FindTunnels returns rooms that bridge two wings.
	FindTunnels(ctx context.Context, sess database.SessionRunner, workspaceID, wingA, wingB string) ([]Tunnel, error)

	// GraphStats returns palace graph overview statistics.
	GraphStats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*Stats, error)
}
