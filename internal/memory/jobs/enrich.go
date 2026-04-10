package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/extract"
	memrepo "github.com/Crawbl-AI/crawbl-backend/internal/memory/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

const (
	// enrichBatchSize caps how many drawers are enriched per sweep so a
	// backlog spike cannot monopolise the worker.
	enrichBatchSize = 100
	// enrichPerDrawerTimeout bounds the single-drawer LLM extract call so
	// one slow upstream response cannot stall the whole batch.
	enrichPerDrawerTimeout = 15 * time.Second
)

// EnrichDeps holds dependencies for the memory enrichment sweep. It
// deliberately mirrors ProcessDeps so jobs/process.go helpers can be
// reused where practical.
type EnrichDeps struct {
	DB            *dbr.Connection
	DrawerRepo    memrepo.DrawerRepo
	KGRepo        memrepo.KGRepo
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

	entityCount, tripleCount := linkEnrichEntities(ctx, sess, deps, d.WorkspaceID, classification)

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

// linkEnrichEntities wires an LLMClassification's entities and triples
// into the KG, returning the successfully-inserted counts so the caller
// can write them back onto the drawer. Mirrors jobs/process.go's
// linkEntities but without the fire-and-forget error discard.
func linkEnrichEntities(ctx context.Context, sess database.SessionRunner, deps EnrichDeps, workspaceID string, classification *extract.LLMClassification) (int, int) {
	if deps.KGRepo == nil {
		return 0, 0
	}
	entityCount := 0
	for _, entity := range classification.Entities {
		if _, err := deps.KGRepo.AddEntity(ctx, sess, workspaceID, entity.Name, entity.Type, "{}"); err != nil {
			deps.Logger.WarnContext(ctx, "memory-enrich: add entity failed",
				slog.String("workspace_id", workspaceID),
				slog.String("entity", entity.Name),
				slog.String("error", err.Error()),
			)
			continue
		}
		entityCount++
	}
	tripleCount := 0
	for _, triple := range classification.Triples {
		t := &memory.Triple{
			WorkspaceID: workspaceID,
			Subject:     triple.Subject,
			Predicate:   triple.Predicate,
			Object:      triple.Object,
			Confidence:  1.0,
		}
		if _, err := deps.KGRepo.AddTriple(ctx, sess, workspaceID, t); err != nil {
			deps.Logger.WarnContext(ctx, "memory-enrich: add triple failed",
				slog.String("workspace_id", workspaceID),
				slog.String("subject", triple.Subject),
				slog.String("predicate", triple.Predicate),
				slog.String("error", err.Error()),
			)
			continue
		}
		tripleCount++
	}
	return entityCount, tripleCount
}
