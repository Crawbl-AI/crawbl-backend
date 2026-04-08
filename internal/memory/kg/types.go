// Package kg provides the knowledge graph operations for MemPalace,
// storing entity nodes and relationship triples in PostgreSQL.
package kg

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// Graph defines the knowledge graph operations.
type Graph interface {
	// AddEntity upserts an entity node.
	AddEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, entityType string, properties string) (string, error)

	// AddTriple adds a relationship triple. Auto-creates entities if they don't exist.
	// Returns the triple ID. If an identical active triple exists, returns its ID without inserting.
	AddTriple(ctx context.Context, sess database.SessionRunner, workspaceID string, t *memory.Triple) (string, error)

	// Invalidate marks a relationship as no longer valid by setting valid_to.
	Invalidate(ctx context.Context, sess database.SessionRunner, workspaceID, subject, predicate, object, ended string) error

	// QueryEntity returns all relationships for an entity.
	// direction: "outgoing", "incoming", or "both"
	// asOf: optional date filter (YYYY-MM-DD) — only facts valid at that date
	QueryEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, asOf, direction string) ([]memory.TripleResult, error)

	// QueryRelationship returns all triples with a given predicate.
	QueryRelationship(ctx context.Context, sess database.SessionRunner, workspaceID, predicate, asOf string) ([]memory.TripleResult, error)

	// Timeline returns facts in chronological order, optionally for one entity.
	Timeline(ctx context.Context, sess database.SessionRunner, workspaceID, entityName string) ([]memory.TripleResult, error)

	// Stats returns knowledge graph statistics.
	Stats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.KGStats, error)
}
