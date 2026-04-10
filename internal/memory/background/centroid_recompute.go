package background

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/jobs"
)

// NewCentroidRecomputeWorker constructs a centroid-recompute worker
// bound to the given dependencies. The business logic lives in
// internal/memory/jobs.RunCentroidRecompute.
func NewCentroidRecomputeWorker(deps Deps) *CentroidRecomputeWorker {
	return &CentroidRecomputeWorker{deps: deps}
}

// Work executes one pass of the centroid recompute pipeline: scan the
// last 90 days of LLM-labelled drawers per memory type, compute the
// element-wise average embedding, and upsert the centroid row when its
// source hash changed.
func (w *CentroidRecomputeWorker) Work(ctx context.Context, job *river.Job[CentroidRecomputeArgs]) error {
	result, err := jobs.RunCentroidRecompute(ctx, jobs.CentroidRecomputeDeps{
		DB:           w.deps.DB,
		DrawerRepo:   w.deps.DrawerRepo,
		CentroidRepo: w.deps.CentroidRepo,
		Logger:       w.deps.Logger,
	})
	if err != nil {
		w.deps.Logger.Error("memory-centroid-recompute job failed",
			slog.Int64("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("run centroid recompute: %w", err)
	}
	w.deps.Logger.Info("memory-centroid-recompute: complete",
		slog.Int64("job_id", job.ID),
		slog.Int("types_updated", result.Updated),
		slog.Int("types_unchanged", result.Unchanged),
		slog.Int("types_skipped", result.Skipped),
	)
	return nil
}
