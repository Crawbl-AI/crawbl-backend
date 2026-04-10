// Package background runs MemPalace periodic work as in-process River jobs,
// replacing the previous K8s CronJob model.
package background

import (
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/drawer"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/kg"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

// Queue names used by MemPalace background workers. These map to River queue
// config in Client.
const (
	QueueMemoryProcess  = "memory_process"
	QueueMemoryMaintain = "memory_maintain"
)

// Deps bundles everything the River workers need to run MemPalace jobs.
// It mirrors the dependencies previously constructed in the two standalone
// cmd/crawbl/jobs/* binaries, so the old entrypoints can be deleted without
// losing any behavior.
type Deps struct {
	DB            *dbr.Connection
	DrawerRepo    drawer.Repo
	KGGraph       kg.Graph
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
