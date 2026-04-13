// Package jobs — ports.go declares the narrow repository contracts the
// memory background jobs (process/maintain/enrich/centroid) depend on.
// Per project convention, these interfaces live at the consumer, not
// the producer: the concrete Postgres structs in
// internal/orchestrator/memory/repo/... satisfy them implicitly via
// structural typing.
package jobs

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// drawerStore is the drawer subset the memory jobs use across process,
// maintain, enrich, and centroid-recompute pipelines. It mirrors exactly
// the call sites inside this package.
type drawerStore interface {
	ActiveWorkspaces(ctx context.Context, sess database.SessionRunner, withinHours int) ([]string, error)
	ListByState(ctx context.Context, sess database.SessionRunner, workspaceID, state string, limit int) ([]memory.Drawer, error)
	ClaimForProcessing(ctx context.Context, sess database.SessionRunner, workspaceID string, ids []string) error
	UpdateClassification(ctx context.Context, sess database.SessionRunner, opts drawerrepo.UpdateClassificationOpts) error
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

// kgStore is the knowledge-graph subset memory jobs use.
type kgStore interface {
	AddEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, entityType, properties string) (string, error)
	AddTriple(ctx context.Context, sess database.SessionRunner, workspaceID string, t *memory.Triple) (string, error)
}

// centroidStore is the centroid subset the weekly centroid-recompute job
// depends on.
type centroidStore interface {
	GetAll(ctx context.Context, sess database.SessionRunner) ([]memory.MemoryTypeCentroid, error)
	Upsert(ctx context.Context, sess database.SessionRunner, rows []memory.MemoryTypeCentroid) error
}
