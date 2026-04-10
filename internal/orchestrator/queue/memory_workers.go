package queue

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/jobs"
)

// NewMemoryProcessWorker constructs a memory_process worker bound to
// the given dependencies. Thin adapter over jobs.RunProcess.
func NewMemoryProcessWorker(deps Deps) *MemoryProcessWorker {
	return &MemoryProcessWorker{deps: deps}
}

// Work executes one sweep of the memory processing pipeline.
func (w *MemoryProcessWorker) Work(ctx context.Context, job *river.Job[MemoryProcessArgs]) error {
	w.deps.Logger.InfoContext(ctx, "memory-process: start",
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.String("queue", job.Queue),
		slog.Int("attempt", job.Attempt),
	)
	result, err := jobs.RunProcess(ctx, jobs.ProcessDeps{
		DB:            w.deps.DB,
		DrawerRepo:    w.deps.DrawerRepo,
		KGRepo:        w.deps.KGRepo,
		LLMClassifier: w.deps.LLMClassifier,
		Embedder:      w.deps.Embedder,
	})
	if err != nil {
		w.deps.Logger.ErrorContext(ctx, "memory-process: failed",
			slog.Int64("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("run process: %w", err)
	}
	w.deps.Logger.InfoContext(ctx, "memory-process: complete",
		slog.Int64("job_id", job.ID),
		slog.Int("processed", result.Processed),
		slog.Int("failed", result.Failed),
	)
	return nil
}

// NewMemoryMaintainWorker constructs a memory_maintain worker bound to
// the given dependencies. Thin adapter over jobs.RunMaintain.
func NewMemoryMaintainWorker(deps Deps) *MemoryMaintainWorker {
	return &MemoryMaintainWorker{deps: deps}
}

// Work executes one sweep of the memory maintenance pipeline.
func (w *MemoryMaintainWorker) Work(ctx context.Context, job *river.Job[MemoryMaintainArgs]) error {
	w.deps.Logger.InfoContext(ctx, "memory-maintain: start",
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.String("queue", job.Queue),
		slog.Int("attempt", job.Attempt),
	)
	result, err := jobs.RunMaintain(ctx, jobs.MaintainDeps{
		DB:         w.deps.DB,
		DrawerRepo: w.deps.DrawerRepo,
	})
	if err != nil {
		w.deps.Logger.ErrorContext(ctx, "memory-maintain: failed",
			slog.Int64("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("run maintain: %w", err)
	}
	w.deps.Logger.InfoContext(ctx, "memory-maintain: complete",
		slog.Int64("job_id", job.ID),
		slog.Int("workspaces", result.Workspaces),
		slog.Int("decayed", result.Decayed),
		slog.Int("pruned", result.Pruned),
	)
	return nil
}

// NewMemoryEnrichWorker constructs a memory_enrich worker bound to the
// given dependencies. Thin adapter over jobs.RunEnrich.
func NewMemoryEnrichWorker(deps Deps) *MemoryEnrichWorker {
	return &MemoryEnrichWorker{deps: deps}
}

// Work pulls up to N processed-but-unenriched drawers, calls the LLM
// extractor once per drawer, wires KG entities + triples, and updates
// entity_count / triple_count so the partial index loses interest in
// the row.
func (w *MemoryEnrichWorker) Work(ctx context.Context, job *river.Job[MemoryEnrichArgs]) error {
	w.deps.Logger.InfoContext(ctx, "memory-enrich: start",
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.String("queue", job.Queue),
		slog.Int("attempt", job.Attempt),
	)
	result, err := jobs.RunEnrich(ctx, jobs.EnrichDeps{
		DB:            w.deps.DB,
		DrawerRepo:    w.deps.DrawerRepo,
		KGRepo:        w.deps.KGRepo,
		LLMClassifier: w.deps.LLMClassifier,
		Logger:        w.deps.Logger,
	})
	if err != nil {
		w.deps.Logger.ErrorContext(ctx, "memory-enrich: failed",
			slog.Int64("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("run enrich: %w", err)
	}
	w.deps.Logger.InfoContext(ctx, "memory-enrich: complete",
		slog.Int64("job_id", job.ID),
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
	)
	return nil
}

// NewMemoryCentroidRecomputeWorker constructs a centroid_recompute
// worker bound to the given dependencies. Thin adapter over
// jobs.RunCentroidRecompute.
func NewMemoryCentroidRecomputeWorker(deps Deps) *MemoryCentroidRecomputeWorker {
	return &MemoryCentroidRecomputeWorker{deps: deps}
}

// Work scans the last 90 days of LLM-labelled drawers per memory type,
// computes the element-wise average embedding, and upserts the centroid
// row when its source hash has changed since the previous run.
func (w *MemoryCentroidRecomputeWorker) Work(ctx context.Context, job *river.Job[MemoryCentroidRecomputeArgs]) error {
	w.deps.Logger.InfoContext(ctx, "memory-centroid-recompute: start",
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.String("queue", job.Queue),
		slog.Int("attempt", job.Attempt),
	)
	result, err := jobs.RunCentroidRecompute(ctx, jobs.CentroidRecomputeDeps{
		DB:           w.deps.DB,
		DrawerRepo:   w.deps.DrawerRepo,
		CentroidRepo: w.deps.CentroidRepo,
		Logger:       w.deps.Logger,
	})
	if err != nil {
		w.deps.Logger.ErrorContext(ctx, "memory-centroid-recompute: failed",
			slog.Int64("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("run centroid recompute: %w", err)
	}
	w.deps.Logger.InfoContext(ctx, "memory-centroid-recompute: complete",
		slog.Int64("job_id", job.ID),
		slog.Int("types_updated", result.Updated),
		slog.Int("types_unchanged", result.Unchanged),
		slog.Int("types_skipped", result.Skipped),
	)
	return nil
}
