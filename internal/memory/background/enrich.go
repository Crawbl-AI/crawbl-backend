package background

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/jobs"
)

// NewEnrichWorker constructs an enrichment worker bound to the given
// dependencies. The business logic lives in internal/memory/jobs.RunEnrich.
func NewEnrichWorker(deps Deps) *EnrichWorker {
	return &EnrichWorker{deps: deps}
}

// Work executes one sweep of the memory enrichment pipeline: pull up to
// N processed-but-unenriched drawers, call the LLM extractor once per
// drawer, wire KG entities + triples, and update entity_count /
// triple_count so the partial index loses interest in the row.
func (w *EnrichWorker) Work(ctx context.Context, job *river.Job[EnrichArgs]) error {
	result, err := jobs.RunEnrich(ctx, jobs.EnrichDeps{
		DB:            w.deps.DB,
		DrawerRepo:    w.deps.DrawerRepo,
		KGRepo:        w.deps.KGRepo,
		LLMClassifier: w.deps.LLMClassifier,
		Logger:        w.deps.Logger,
	})
	if err != nil {
		w.deps.Logger.Error("memory-enrich job failed",
			slog.Int64("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("run enrich: %w", err)
	}
	w.deps.Logger.Info("memory-enrich: complete",
		slog.Int64("job_id", job.ID),
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
	)
	return nil
}
