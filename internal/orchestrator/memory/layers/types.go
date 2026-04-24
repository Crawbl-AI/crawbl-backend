// Package layers implements the MemPalace memory stack: L0 identity,
// L1 essential story, L2 on-demand retrieval, and L3 semantic search.
package layers

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// Stack provides the unified memory interface.
// WakeUp returns L0+L1 (~600-900 tokens).
// Recall returns L2 on-demand retrieval.
// Search returns L3 deep semantic search.
type Stack interface {
	// WakeUp generates the wake-up text: L0 (identity) + L1 (essential story).
	// Inject this into the system prompt. Optional wing filter for project-specific wake-up.
	WakeUp(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) (string, error)

	// Recall retrieves on-demand L2 memories filtered by wing and/or room.
	Recall(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) (string, error)

	// Search performs deep L3 semantic search.
	Search(ctx context.Context, sess database.SessionRunner, workspaceID, query, wing, room string, limit int) (string, error)
}

// drawerStore is the subset of drawer persistence the layers package
// calls into — hybrid search, wing/room lookups, importance-sorted L1,
// arbitrary-filter L2, semantic L3, and batch access-touching.
type drawerStore interface {
	Search(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, wing, room string, limit int) ([]memory.DrawerSearchResult, error)
	SearchHybrid(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, queryTerms []string, limit int) ([]memory.HybridSearchResult, error)
	GetTopByImportance(ctx context.Context, sess database.SessionRunner, workspaceID, wing string, limit int) ([]memory.Drawer, error)
	GetByWingRoom(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) ([]memory.Drawer, error)
	TouchAccessBatch(ctx context.Context, sess database.SessionRunner, workspaceID string, drawerIDs []string) error
}

// identityGetter is the identity-row subset the L0 renderer reads to
// surface each workspace's pinned identity text.
type identityGetter interface {
	Get(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.Identity, error)
}
