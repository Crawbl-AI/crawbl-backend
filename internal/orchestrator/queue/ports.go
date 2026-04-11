// Package queue — ports.go declares the narrow repository contracts
// this package depends on. Per project convention, interfaces are defined
// at the consumer, not the producer.
//
// The River workers in this package drive three distinct repo surfaces:
//
//   - Memory repos (drawer / KG / centroid) for the memory jobs. These
//     are forwarded to internal/orchestrator/memory/jobs which has its
//     own consumer-side interfaces; queue only needs enough method
//     surface to pass handles through.
//   - MessageRepo for the stale-pending-message cleanup worker. Only
//     FailStalePending is called here.
//
// Keeping this file package-private means callers cannot accidentally
// widen what the queue layer depends on without also adding a matching
// call site.
package queue

import (
	"context"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// drawerStore is the superset of drawer methods the queue workers hand
// off to the memory jobs package. The union is unavoidable because each
// job (process, maintain, enrich, centroid) calls into a different
// subset; the queue workers fan out through a shared Deps. Any method
// listed here is called from at least one worker via
// internal/orchestrator/memory/jobs.
type drawerStore interface {
	ActiveWorkspaces(ctx context.Context, sess database.SessionRunner, withinHours int) ([]string, error)
	ListByState(ctx context.Context, sess database.SessionRunner, workspaceID, state string, limit int) ([]memory.Drawer, error)
	UpdateClassification(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, memoryType, summary, room string, importance float64) error
	UpdateEmbedding(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, embedding []float32) error
	UpdateState(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, state string) error
	Search(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, wing, room string, limit int) ([]memory.DrawerSearchResult, error)
	SetSupersededBy(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, supersededBy string) error
	SetClusterID(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, clusterID string) error
	IncrementRetryCount(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error
	DecayImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, olderThanDays, skipAccessedWithinDays int, factor, floor float64) (int, error)
	PruneLowImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, threshold float64, minAccessCount, keepMin int) (int, error)
	ListEnrichCandidates(ctx context.Context, sess database.SessionRunner, limit int) ([]memory.Drawer, error)
	UpdateEnrichment(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, entityCount, tripleCount int) error
	ListCentroidTrainingSamples(ctx context.Context, sess database.SessionRunner, windowDays, topN int) ([]memory.CentroidTrainingSample, error)
}

// kgStore is the knowledge-graph subset queue workers forward into the
// memory jobs package.
type kgStore interface {
	AddEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, entityType, properties string) (string, error)
	AddTriple(ctx context.Context, sess database.SessionRunner, workspaceID string, t *memory.Triple) (string, error)
}

// centroidStore is the centroid subset used by the weekly centroid
// recompute worker.
type centroidStore interface {
	GetAll(ctx context.Context, sess database.SessionRunner) ([]memory.MemoryTypeCentroid, error)
	Upsert(ctx context.Context, sess database.SessionRunner, rows []memory.MemoryTypeCentroid) error
}

// messageStore is the message subset used by the stale-pending cleanup
// worker: a single UPDATE call that flips orphaned placeholders to
// failed status so the mobile UI never hangs.
type messageStore interface {
	FailStalePending(ctx context.Context, sess orchestratorrepo.SessionRunner, cutoff time.Time) (int, *merrors.Error)
}
