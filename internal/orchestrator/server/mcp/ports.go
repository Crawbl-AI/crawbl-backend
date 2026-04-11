// Package mcp — ports.go declares the narrow repository contracts the
// MCP tool handlers depend on. Per project convention, interfaces live
// at the consumer, not the producer.
package mcp

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// drawerStore is the drawer subset MCP memory tools use across the
// status, list-wings, list-rooms, taxonomy, search, duplicate, add,
// delete, and diary-read surfaces.
type drawerStore interface {
	Count(ctx context.Context, sess database.SessionRunner, workspaceID string) (int, error)
	ListWings(ctx context.Context, sess database.SessionRunner, workspaceID string) ([]memory.WingCount, error)
	ListRooms(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) ([]memory.RoomCount, error)
	Search(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, wing, room string, limit int) ([]memory.DrawerSearchResult, error)
	CheckDuplicate(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32, threshold float64, limit int) ([]memory.DrawerSearchResult, error)
	Add(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error
	Delete(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error
	BoostImportance(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, delta, maxImportance float64) error
	GetByWingRoom(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) ([]memory.Drawer, error)
}

// kgStore is the knowledge-graph subset MCP tools use for entity
// queries, triple additions, invalidation, timelines, and stats.
type kgStore interface {
	QueryEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, asOf, direction string) ([]memory.TripleResult, error)
	AddTriple(ctx context.Context, sess database.SessionRunner, workspaceID string, t *memory.Triple) (string, error)
	Invalidate(ctx context.Context, sess database.SessionRunner, workspaceID, subject, predicate, object, ended string) error
	Timeline(ctx context.Context, sess database.SessionRunner, workspaceID, entityName string) ([]memory.TripleResult, error)
	Stats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.KGStats, error)
}

// palaceGraphStore is the navigation subset MCP tools use for graph
// traversal and bridge detection.
type palaceGraphStore interface {
	Traverse(ctx context.Context, sess database.SessionRunner, workspaceID, startRoom string, maxHops int) ([]memory.TraversalResult, error)
	FindTunnels(ctx context.Context, sess database.SessionRunner, workspaceID, wingA, wingB string) ([]memory.Tunnel, error)
	GraphStats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.PalaceGraphStats, error)
}

// identityStore is the identity subset MCP tools use to pin a
// workspace's identity text.
type identityStore interface {
	Set(ctx context.Context, sess database.SessionRunner, workspaceID, content string) error
}
