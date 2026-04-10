// Package background runs MemPalace periodic work as in-process River jobs,
// replacing the previous K8s CronJob model.
package background

import (
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/extract"
	memrepo "github.com/Crawbl-AI/crawbl-backend/internal/memory/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

// Queue names used by MemPalace background workers. These map to River queue
// config in Client.
const (
	QueueMemoryProcess  = "memory_process"
	QueueMemoryMaintain = "memory_maintain"
	QueueMemoryEnrich   = "memory_enrich"
	QueueMemoryCentroid = "memory_centroid"

	// processConcurrency bounds concurrent LLM classify calls inside the
	// memory_process queue. Tunable via orchestrator env in a later phase;
	// kept as a const here until we see real contention.
	processConcurrency = 3

	// processSweepInterval is the periodic sweep cadence for the cold
	// classification pipeline. After Phase 0 the auto-ingest hot path no
	// longer enqueues follow-up ProcessArgs, so this interval is the
	// primary trigger for raw-drawer classification.
	processSweepInterval = time.Minute

	// enrichSweepInterval is the cadence at which the enrichment worker
	// picks up processed drawers that skipped the LLM path and still need
	// KG entity linking.
	enrichSweepInterval = 10 * time.Minute

	// centroidRecomputeDedupWindow collapses duplicate centroid recompute
	// inserts to at most one per day. The weekly cron fires on Sunday
	// 03:00 UTC, so a 24h uniqueness window is wider than the schedule
	// but still prevents a manual enqueue storm from re-running the
	// expensive recompute in under a day.
	centroidRecomputeDedupWindow = 24 * time.Hour
)

// ProcessWorker is the River worker that runs the cold memory processing
// pipeline: classify raw drawers, link KG entities, cluster, detect conflicts.
// The business logic lives in internal/memory/jobs.RunProcess — this worker
// is a thin adapter that builds ProcessDeps and reports metrics.
type ProcessWorker struct {
	river.WorkerDefaults[ProcessArgs]
	deps Deps
}

// MaintainWorker is the River worker that runs the MemPalace maintenance
// pipeline: importance decay and low-importance pruning across all active
// workspaces. The business logic lives in internal/memory/jobs.RunMaintain —
// this worker is a thin adapter that builds MaintainDeps and reports metrics.
type MaintainWorker struct {
	river.WorkerDefaults[MaintainArgs]
	deps Deps
}

// Deps bundles everything the River-backed MemPalace workers need to
// run cold classification, KG enrichment, maintenance, and centroid
// recompute. Auto-ingest is no longer in this set — it runs in-process
// under internal/memory/autoingest so the chat-turn hot path never
// writes to river_job.
type Deps struct {
	DB            *dbr.Connection
	DrawerRepo    memrepo.DrawerRepo
	KGRepo        memrepo.KGRepo
	CentroidRepo  memrepo.CentroidRepo
	LLMClassifier extract.LLMClassifier
	Embedder      embed.Embedder
	Logger        *slog.Logger
}

// ProcessArgs triggers a single batch run of RunProcess over all active
// workspaces. The struct is intentionally empty: we periodically sweep all
// raw drawers per workspace, rather than addressing a single drawer ID, so
// concurrent inserts from auto-ingest are deduped by River via UniqueOpts.
type ProcessArgs struct{}

// Kind implements river.JobArgs.
func (ProcessArgs) Kind() string { return "memory_process" }

// InsertOpts implements the optional river.JobArgsWithInsertOpts — every
// process job goes onto the memory_process queue, and concurrent inserts
// within a 60s window are deduped.
func (ProcessArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueMemoryProcess,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 60 * time.Second,
		},
	}
}

// MaintainArgs triggers a single batch run of RunMaintain (decay + prune)
// over all active workspaces.
type MaintainArgs struct{}

// Kind implements river.JobArgs.
func (MaintainArgs) Kind() string { return "memory_maintain" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (MaintainArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueMemoryMaintain,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: time.Hour,
		},
	}
}

// EnrichArgs triggers a sweep of the memory enrichment worker which
// backfills KG entity/triple links for drawers that took the heuristic
// or centroid fast path. Runs periodically (every 10 minutes) and
// dedupes overlapping runs via the standard ByArgs+ByPeriod pattern.
type EnrichArgs struct{}

// Kind implements river.JobArgs.
func (EnrichArgs) Kind() string { return "memory_enrich" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (EnrichArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueMemoryEnrich,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: enrichSweepInterval,
		},
	}
}

// EnrichWorker is the River worker adapter for RunEnrich.
type EnrichWorker struct {
	river.WorkerDefaults[EnrichArgs]
	deps Deps
}

// CentroidRecomputeArgs triggers the weekly centroid rebuild over the
// last 90 days of LLM-labelled drawers. The struct is intentionally
// empty: a single run processes every memory type in one pass.
type CentroidRecomputeArgs struct{}

// Kind implements river.JobArgs.
func (CentroidRecomputeArgs) Kind() string { return "memory_centroid_recompute" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (CentroidRecomputeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueMemoryCentroid,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: centroidRecomputeDedupWindow,
		},
	}
}

// CentroidRecomputeWorker is the River worker adapter for
// jobs.RunCentroidRecompute.
type CentroidRecomputeWorker struct {
	river.WorkerDefaults[CentroidRecomputeArgs]
	deps Deps
}
