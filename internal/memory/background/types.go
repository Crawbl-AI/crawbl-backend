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
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

// Queue names used by MemPalace background workers. These map to River queue
// config in Client.
const (
	QueueMemoryProcess  = "memory_process"
	QueueMemoryMaintain = "memory_maintain"

	// processConcurrency bounds concurrent LLM classify calls inside the
	// memory_process queue. Tunable via orchestrator env in a later phase;
	// kept as a const here until we see real contention.
	processConcurrency = 3

	// processSweepInterval is the safety-net periodic sweep cadence. The
	// primary trigger for memory_process is an ad-hoc Insert from the
	// auto-ingest worker — this interval only catches drawers whose insert
	// slipped through (e.g. crash between AddIdempotent and Insert).
	processSweepInterval = time.Minute
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

// Deps bundles everything the River workers need to run MemPalace jobs:
// the cold processing pipeline, the maintenance sweep, and the auto-ingest
// worker that replaces the chatservice in-process goroutine.
type Deps struct {
	DB              *dbr.Connection
	DrawerRepo      memrepo.DrawerRepo
	KGRepo          memrepo.KGRepo
	LLMClassifier   extract.LLMClassifier
	Classifier      extract.Classifier
	Embedder        embed.Embedder
	MemoryPublisher *queue.MemoryPublisher
	Logger          *slog.Logger
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

// InsertOpts routes maintain jobs onto their own queue and prevents duplicate
// concurrent runs via a 1-hour uniqueness window.
func (MaintainArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueMemoryMaintain,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: time.Hour,
		},
	}
}
