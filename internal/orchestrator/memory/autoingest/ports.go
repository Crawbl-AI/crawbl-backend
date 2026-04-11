// Package autoingest — ports.go declares the narrow repository contracts
// the memory auto-ingest hot path depends on. Per project convention,
// interfaces are defined at the consumer, not the producer.
package autoingest

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// drawerStore is the drawer subset the auto-ingest worker uses:
// idempotent add for the hot path plus a duplicate-check probe before
// inserting.
type drawerStore interface {
	AddIdempotent(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error
	CheckDuplicate(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32, threshold float64, limit int) ([]memory.DrawerSearchResult, error)
}

// centroidStore is the centroid subset used by the Phase-2 nearest-type
// classifier. Optional at runtime — the worker no-ops when nil.
type centroidStore interface {
	NearestType(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32) (memType string, similarity float64, ok bool, err error)
}
