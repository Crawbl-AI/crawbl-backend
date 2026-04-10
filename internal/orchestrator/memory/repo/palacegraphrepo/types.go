// Package palacegraphrepo provides palace graph navigation operations,
// including BFS traversal and tunnel detection across memory rooms.
package palacegraphrepo

// RoomNode is the internal graph-node shape built from memory_drawers
// groupings. It is cached per workspace inside graphCache and consumed by
// Traverse / FindTunnels / GraphStats to project domain results.
type RoomNode struct {
	Room  string   `json:"room"`
	Wings []string `json:"wings"`
	Halls []string `json:"halls"`
	Count int      `json:"count"`
}
