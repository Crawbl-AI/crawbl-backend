package background

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/jobs"
)

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

// NewProcessWorker constructs a worker bound to the given dependencies.
func NewProcessWorker(deps Deps) *ProcessWorker {
	return &ProcessWorker{deps: deps}
}

// Work executes one sweep of the memory processing pipeline.
func (w *ProcessWorker) Work(ctx context.Context, job *river.Job[ProcessArgs]) error {
	result, err := jobs.RunProcess(ctx, jobs.ProcessDeps{
		DB:            w.deps.DB,
		DrawerRepo:    w.deps.DrawerRepo,
		KGGraph:       w.deps.KGGraph,
		LLMClassifier: w.deps.LLMClassifier,
		Embedder:      w.deps.Embedder,
	})
	if err != nil {
		w.deps.Logger.Error("memory-process job failed",
			slog.Int64("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("run process: %w", err)
	}
	w.deps.Logger.Info("memory-process job complete",
		slog.Int64("job_id", job.ID),
		slog.Int("processed", result.Processed),
		slog.Int("failed", result.Failed),
	)
	return nil
}
