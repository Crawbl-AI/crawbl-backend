package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/defaults"
)

const (
	// enrichBatchSize caps how many drawers are enriched per sweep so a
	// backlog spike cannot monopolise the worker.
	enrichBatchSize = 100
)

var (
	// enrichPerDrawerTimeout bounds the single-drawer LLM extract call so
	// one slow upstream response cannot stall the whole batch.
	enrichPerDrawerTimeout = defaults.MediumTimeout
)

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

// RunEnrich drains up to enrichBatchSize processed-but-unenriched
// drawers, extracts entities + triples with the LLM, and updates the
// drawer's entity_count / triple_count so the partial index stops
// matching it. Any per-drawer failure is logged and the loop continues
// — the next sweep will try again because the partial-index predicate
// still matches.
func RunEnrich(ctx context.Context, deps EnrichDeps) (*EnrichResult, error) {
	if deps.LLMClassifier == nil {
		return &EnrichResult{}, nil
	}
	sess := deps.DB.NewSession(nil)

	candidates, err := deps.DrawerRepo.ListEnrichCandidates(ctx, sess, enrichBatchSize)
	if err != nil {
		return nil, fmt.Errorf("list enrich candidates: %w", err)
	}
	if len(candidates) == 0 {
		return &EnrichResult{}, nil
	}

	result := &EnrichResult{}
	for i := range candidates {
		enrichOneDrawer(ctx, sess, deps, &candidates[i], result)
	}
	return result, nil
}

// enrichOneDrawer runs LLM extract on one drawer, wires the KG nodes,
// and writes entity_count / triple_count back. Soft-fails on any error
// so one bad row never stops the sweep.
func enrichOneDrawer(ctx context.Context, sess database.SessionRunner, deps EnrichDeps, d *memory.Drawer, result *EnrichResult) {
	drawerCtx, cancel := context.WithTimeout(ctx, enrichPerDrawerTimeout)
	defer cancel()

	classification, err := deps.LLMClassifier.ClassifyAndExtract(drawerCtx, d.Content)
	if err != nil {
		result.Skipped++
		deps.Logger.WarnContext(ctx, "memory-enrich: classify failed",
			slog.String("workspace_id", d.WorkspaceID),
			slog.String("drawer_id", d.ID),
			slog.String("error", err.Error()),
		)
		return
	}
	if classification == nil {
		result.Skipped++
		return
	}

	entityCount, tripleCount := linkAndCount(ctx, sess, deps.KGRepo, d.WorkspaceID, d.Hall, classification)

	if err := deps.DrawerRepo.UpdateEnrichment(ctx, sess, d.WorkspaceID, d.ID, entityCount, tripleCount); err != nil {
		deps.Logger.WarnContext(ctx, "memory-enrich: update failed",
			slog.String("workspace_id", d.WorkspaceID),
			slog.String("drawer_id", d.ID),
			slog.String("error", err.Error()),
		)
		result.Skipped++
		return
	}
	result.Processed++
}
