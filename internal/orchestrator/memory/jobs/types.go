// Package jobs provides standalone K8s CronJob entry points for MemPalace memory processing.
package jobs

import (
	"context"
	"log/slog"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/kgrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/config"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

// Consts.

const (
	activeWorkspaceHours = 24
	rawDrawerBatchSize   = 50
	importanceScale      = 5.0
)

const (
	// centroidSampleWindowDays is the lookback for LLM-labelled drawers
	// when training a centroid. 90 days balances drift response against
	// sample size in a busy workspace.
	centroidSampleWindowDays = 90
	// centroidTopN caps the sample cohort per memory type so one noisy
	// type cannot dominate compute and one quiet type cannot be drowned.
	centroidTopN = 500
	// centroidMemoryTypeHint is the initial capacity hint for the
	// per-type grouping map. Matches the count of declared memory
	// types in memory/types.go (decision, preference, milestone,
	// problem, emotional, fact, task) so the map never has to grow.
	centroidMemoryTypeHint = 7
)

const (
	// enrichBatchSize caps how many drawers are enriched per sweep so a
	// backlog spike cannot monopolise the worker.
	enrichBatchSize = 100
)

const decaySkipRecentDays = 7

// Vars.

var (
	// enrichPerDrawerTimeout bounds the single-drawer LLM extract call so
	// one slow upstream response cannot stall the whole batch.
	enrichPerDrawerTimeout = config.MediumTimeout
)

// Types — interfaces.

// DrawerStore is the drawer subset the memory jobs use across process,
// maintain, enrich, and centroid-recompute pipelines. It mirrors exactly
// the call sites inside this package.
type DrawerStore interface {
	ActiveWorkspaces(ctx context.Context, sess database.SessionRunner, withinHours int) ([]string, error)
	ListByState(ctx context.Context, sess database.SessionRunner, workspaceID, state string, limit int) ([]memory.Drawer, error)
	ClaimForProcessing(ctx context.Context, sess database.SessionRunner, workspaceID string, ids []string) error
	UpdateClassification(ctx context.Context, sess database.SessionRunner, opts drawerrepo.UpdateClassificationOpts) error
	UpdateEmbedding(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, embedding []float32) error
	UpdateState(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, state string) error
	Search(ctx context.Context, sess database.SessionRunner, opts drawerrepo.SearchOpts) ([]memory.DrawerSearchResult, error)
	SetSupersededBy(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, supersededBy string) error
	SetClusterID(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, clusterID string) error
	IncrementRetryCount(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error
	DecayImportance(ctx context.Context, sess database.SessionRunner, opts drawerrepo.DecayImportanceOpts) (int, error)
	PruneLowImportance(ctx context.Context, sess database.SessionRunner, opts drawerrepo.PruneLowImportanceOpts) (int, error)
	ListEnrichCandidates(ctx context.Context, sess database.SessionRunner, limit int) ([]memory.Drawer, error)
	UpdateEnrichment(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, entityCount, tripleCount int) error
	ListCentroidTrainingSamples(ctx context.Context, sess database.SessionRunner, windowDays, topN int) ([]memory.CentroidTrainingSample, error)
}

// KGStore is the knowledge-graph subset memory jobs use.
type KGStore interface {
	AddEntity(ctx context.Context, sess database.SessionRunner, opts kgrepo.AddEntityOpts) (string, error)
	AddTriple(ctx context.Context, sess database.SessionRunner, workspaceID string, t *memory.Triple) (string, error)
}

// CentroidStore is the centroid subset the weekly centroid-recompute job
// depends on.
type CentroidStore interface {
	GetAll(ctx context.Context, sess database.SessionRunner) ([]memory.MemoryTypeCentroid, error)
	Upsert(ctx context.Context, sess database.SessionRunner, rows []memory.MemoryTypeCentroid) error
}

// Types — structs.

// ProcessDeps holds dependencies for the memory processing job.
// Repo fields are typed against consumer-side interfaces declared in
// ports.go.
type ProcessDeps struct {
	DB            *dbr.Connection
	DrawerRepo    DrawerStore
	KGRepo        KGStore
	LLMClassifier extract.LLMClassifier
	Embedder      embed.Embedder
}

// ProcessResult holds the outcome of a processing run.
type ProcessResult struct {
	Processed int
	Failed    int
}

// processSingleDrawerOpts groups the per-drawer parameters for processSingleDrawer.
// ctx and sess remain positional per the project session/opts/repo pattern.
type processSingleDrawerOpts struct {
	Deps            ProcessDeps
	WorkspaceID     string
	Drawer          *memory.Drawer
	Classifications []*extract.LLMClassification
	Idx             int
	BatchErr        error
	Result          *ProcessResult
}

// CentroidRecomputeDeps holds dependencies for the centroid recompute job.
// Repo fields reference consumer-side interfaces declared in ports.go so
// the job layer never imports producer-owned interfaces.
type CentroidRecomputeDeps struct {
	DB           *dbr.Connection
	DrawerRepo   DrawerStore
	CentroidRepo CentroidStore
	Logger       *slog.Logger
}

// CentroidRecomputeResult is the summary line for one centroid sweep.
type CentroidRecomputeResult struct {
	Updated   int
	Unchanged int
	Skipped   int
}

// EnrichDeps holds dependencies for the memory enrichment sweep. It
// deliberately mirrors ProcessDeps so jobs/process.go helpers can be
// reused where practical. Repo fields are consumer-side interfaces from
// ports.go.
type EnrichDeps struct {
	DB            *dbr.Connection
	DrawerRepo    DrawerStore
	KGRepo        KGStore
	LLMClassifier extract.LLMClassifier
	Logger        *slog.Logger
}

// EnrichResult reports one sweep's outcome for metrics + log lines.
// A "remaining backlog" counter is deliberately absent — computing it
// accurately requires a separate COUNT(*) query that is not worth the
// extra round-trip per sweep. Operators can read backlog size directly
// from the idx_drawers_enrich partial index if needed.
type EnrichResult struct {
	Processed int
	Skipped   int
}

// MaintainDeps holds dependencies for the memory maintenance job. The
// DrawerRepo field uses the narrow consumer-side contract in ports.go.
type MaintainDeps struct {
	DB         *dbr.Connection
	DrawerRepo DrawerStore
}

// MaintainResult holds the outcome of a maintenance run.
type MaintainResult struct {
	Workspaces int
	Decayed    int
	Pruned     int
}
