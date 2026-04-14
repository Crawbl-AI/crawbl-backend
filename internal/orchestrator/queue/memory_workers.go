package queue

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/jobs"
)

// runMemoryWork is the shared scaffolding behind every memory River
// worker. It logs start, runs the caller-supplied pipeline, wraps any
// error with opLabel and errVerb, and logs completion using the
// caller-supplied result fields. Keeps one copy of the log-format /
// error-wrap rules for all four workers.
func runMemoryWork[Result any, Args river.JobArgs](
	ctx context.Context,
	logger *slog.Logger,
	opLabel string,
	errVerb string,
	job *river.Job[Args],
	run func(ctx context.Context) (Result, error),
	completeFields func(Result) []any,
) error {
	logger.InfoContext(ctx, opLabel+": start",
		slog.Int64("job_id", job.ID),
		slog.String("kind", job.Kind),
		slog.String("queue", job.Queue),
		slog.Int("attempt", job.Attempt),
	)
	result, err := run(ctx)
	if err != nil {
		logger.ErrorContext(ctx, opLabel+": failed",
			slog.Int64("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%s: %w", errVerb, err)
	}
	attrs := append([]any{slog.Int64("job_id", job.ID)}, completeFields(result)...)
	logger.InfoContext(ctx, opLabel+": complete", attrs...)
	return nil
}

// NewMemoryProcessWorker constructs a memory_process worker bound to
// the given dependencies. Thin adapter over jobs.RunProcess.
func NewMemoryProcessWorker(deps Deps) *MemoryProcessWorker {
	return &MemoryProcessWorker{deps: deps}
}

// Work executes one sweep of the memory processing pipeline.
func (w *MemoryProcessWorker) Work(ctx context.Context, job *river.Job[MemoryProcessArgs]) error {
	return runMemoryWork(ctx, w.deps.Logger, "memory-process", "run process", job,
		func(ctx context.Context) (*jobs.ProcessResult, error) {
			return jobs.RunProcess(ctx, jobs.ProcessDeps{
				DB:            w.deps.DB,
				DrawerRepo:    w.deps.DrawerRepo,
				KGRepo:        w.deps.KGRepo,
				LLMClassifier: w.deps.LLMClassifier,
				Embedder:      w.deps.Embedder,
			})
		},
		func(r *jobs.ProcessResult) []any {
			return []any{slog.Int("processed", r.Processed), slog.Int("failed", r.Failed)}
		},
	)
}

// NewMemoryMaintainWorker constructs a memory_maintain worker bound to
// the given dependencies. Thin adapter over jobs.RunMaintain.
func NewMemoryMaintainWorker(deps Deps) *MemoryMaintainWorker {
	return &MemoryMaintainWorker{deps: deps}
}

// Work executes one sweep of the memory maintenance pipeline.
func (w *MemoryMaintainWorker) Work(ctx context.Context, job *river.Job[MemoryMaintainArgs]) error {
	return runMemoryWork(ctx, w.deps.Logger, "memory-maintain", "run maintain", job,
		func(ctx context.Context) (*jobs.MaintainResult, error) {
			return jobs.RunMaintain(ctx, jobs.MaintainDeps{
				DB:         w.deps.DB,
				DrawerRepo: w.deps.DrawerRepo,
			})
		},
		func(r *jobs.MaintainResult) []any {
			return []any{
				slog.Int("workspaces", r.Workspaces),
				slog.Int("decayed", r.Decayed),
				slog.Int("pruned", r.Pruned),
			}
		},
	)
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
	return runMemoryWork(ctx, w.deps.Logger, "memory-enrich", "run enrich", job,
		func(ctx context.Context) (*jobs.EnrichResult, error) {
			return jobs.RunEnrich(ctx, jobs.EnrichDeps{
				DB:            w.deps.DB,
				DrawerRepo:    w.deps.DrawerRepo,
				KGRepo:        w.deps.KGRepo,
				LLMClassifier: w.deps.LLMClassifier,
				Logger:        w.deps.Logger,
			})
		},
		func(r *jobs.EnrichResult) []any {
			return []any{slog.Int("processed", r.Processed), slog.Int("skipped", r.Skipped)}
		},
	)
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
	return runMemoryWork(ctx, w.deps.Logger, "memory-centroid-recompute", "run centroid recompute", job,
		func(ctx context.Context) (*jobs.CentroidRecomputeResult, error) {
			return jobs.RunCentroidRecompute(ctx, jobs.CentroidRecomputeDeps{
				DB:           w.deps.DB,
				DrawerRepo:   w.deps.DrawerRepo,
				CentroidRepo: w.deps.CentroidRepo,
				Logger:       w.deps.Logger,
			})
		},
		func(r *jobs.CentroidRecomputeResult) []any {
			return []any{
				slog.Int("types_updated", r.Updated),
				slog.Int("types_unchanged", r.Unchanged),
				slog.Int("types_skipped", r.Skipped),
			}
		},
	)
}
