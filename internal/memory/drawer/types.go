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

	// AddIdempotent inserts a drawer with ON CONFLICT DO NOTHING semantics.
	AddIdempotent(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error

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

	// ListByWorkspace returns all drawers for a workspace, ordered by filed_at DESC.
	ListByWorkspace(ctx context.Context, sess database.SessionRunner, workspaceID string, limit, offset int) ([]memory.Drawer, error)

	// ListByState returns drawers in a given state, ordered by created_at ASC.
	// Uses FOR UPDATE SKIP LOCKED for concurrent worker safety.
	ListByState(ctx context.Context, sess database.SessionRunner, workspaceID, state string, limit int) ([]memory.Drawer, error)

	// UpdateState sets the processing state of a drawer.
	UpdateState(ctx context.Context, sess database.SessionRunner, drawerID, state string) error

	// UpdateClassification sets the memory type, summary, room, and importance after LLM classification.
	UpdateClassification(ctx context.Context, sess database.SessionRunner, drawerID, memoryType, summary, room string, importance float64) error

	// SetSupersededBy marks a drawer as superseded by another drawer.
	SetSupersededBy(ctx context.Context, sess database.SessionRunner, drawerID, supersededBy string) error

	// SetClusterID assigns a drawer to a cluster.
	SetClusterID(ctx context.Context, sess database.SessionRunner, drawerID, clusterID string) error

	// TouchAccess updates last_accessed_at and increments access_count for a drawer.
	TouchAccess(ctx context.Context, sess database.SessionRunner, drawerID string) error

	// IncrementRetryCount bumps the retry counter for a drawer.
	IncrementRetryCount(ctx context.Context, sess database.SessionRunner, drawerID string) error

	// DecayImportance reduces importance for old, unaccessed drawers.
	DecayImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, olderThanDays, skipAccessedWithinDays int, factor, floor float64) (int, error)

	// PruneLowImportance deletes low-importance, low-access drawers while keeping a minimum count.
	PruneLowImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, threshold float64, minAccessCount, keepMin int) (int, error)

	// ActiveWorkspaces returns workspace IDs with recent activity.
	ActiveWorkspaces(ctx context.Context, sess database.SessionRunner, withinHours int) ([]string, error)

	// GetByID returns a single drawer by ID within a workspace.
	GetByID(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) (*memory.Drawer, error)
}
