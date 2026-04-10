package background

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/jobs"
)

// MaintainWorker is the River worker that runs the MemPalace maintenance
// pipeline: importance decay and low-importance pruning across all active
// workspaces. The business logic lives in internal/memory/jobs.RunMaintain —
// this worker is a thin adapter that builds MaintainDeps and reports metrics.
type MaintainWorker struct {
	river.WorkerDefaults[MaintainArgs]
	deps Deps
}

// NewMaintainWorker constructs a worker bound to the given dependencies.
func NewMaintainWorker(deps Deps) *MaintainWorker {
	return &MaintainWorker{deps: deps}
}

// Work executes one sweep of the memory maintenance pipeline.
func (w *MaintainWorker) Work(ctx context.Context, job *river.Job[MaintainArgs]) error {
	result, err := jobs.RunMaintain(ctx, jobs.MaintainDeps{
		DB:         w.deps.DB,
		DrawerRepo: w.deps.DrawerRepo,
	})
	if err != nil {
		w.deps.Logger.Error("memory-maintain job failed",
			slog.Int64("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("run maintain: %w", err)
	}
	w.deps.Logger.Info("memory-maintain job complete",
		slog.Int64("job_id", job.ID),
		slog.Int("workspaces", result.Workspaces),
		slog.Int("decayed", result.Decayed),
		slog.Int("pruned", result.Pruned),
	)
	return nil
}
