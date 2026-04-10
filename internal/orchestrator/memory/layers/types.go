// Package layers implements the MemPalace memory stack: L0 identity,
// L1 essential story, L2 on-demand retrieval, and L3 semantic search.
package layers

import (
	"context"

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
