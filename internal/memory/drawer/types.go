// Package drawer provides the vector store for MemPalace memory drawers
// backed by PostgreSQL with pgvector for semantic search.
package drawer

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// Repo defines the drawer persistence operations.
type Repo interface {
	// Add inserts a drawer with its embedding. Checks workspace limits.
	Add(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error

	// Delete removes a drawer by ID within a workspace.
	Delete(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error

	// Search performs semantic vector search using cosine similarity.
	// Returns drawers ordered by similarity (highest first).
	// Filters by wing and/or room if provided.
	Search(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, wing, room string, limit int) ([]memory.DrawerSearchResult, error)

	// CheckDuplicate finds drawers above the similarity threshold.
	CheckDuplicate(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32, threshold float64, limit int) ([]memory.DrawerSearchResult, error)

	// Count returns the total drawer count for a workspace.
	Count(ctx context.Context, sess database.SessionRunner, workspaceID string) (int, error)

	// ListWings returns wings with drawer counts for a workspace.
	ListWings(ctx context.Context, sess database.SessionRunner, workspaceID string) ([]memory.WingCount, error)

	// ListRooms returns rooms with drawer counts, optionally filtered by wing.
	ListRooms(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) ([]memory.RoomCount, error)

	// GetTopByImportance returns the top N drawers by importance for L1 generation.
	// Optionally filtered by wing.
	GetTopByImportance(ctx context.Context, sess database.SessionRunner, workspaceID, wing string, limit int) ([]memory.Drawer, error)

	// GetByWingRoom returns drawers filtered by wing and/or room for L2 retrieval.
	GetByWingRoom(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) ([]memory.Drawer, error)
}
