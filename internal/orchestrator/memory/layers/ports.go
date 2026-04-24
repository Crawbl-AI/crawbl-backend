// Package layers — ports.go declares the narrow repository contracts the
// memory context-rendering layers depend on. Per project convention,
// interfaces are defined at the consumer, not the producer.
package layers

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

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
