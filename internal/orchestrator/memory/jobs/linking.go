// Package jobs — linking.go provides the shared linkAndCount helper used by
// both the hot process pipeline (process.go) and the cold enrich pipeline
// (enrich.go). Keeping the implementation in one place ensures SourceCloset is
// always populated and error handling is consistent across both call sites.
package jobs

import (
	"context"
	"log/slog"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// linkAndCount wires an LLMClassification's entities and triples into the
// knowledge graph, returning the number of each that were successfully
// inserted. The drawer's hall is used as SourceCloset on every triple so the
// hybrid-retrieval guard `WHERE source_closet <> ”` picks them up.
//
// Errors on individual rows are logged and skipped — the caller still receives
// the count of successfully-inserted rows. A nil kgRepo is treated as a no-op
// and both counts return 0.
func linkAndCount(
	ctx context.Context,
	sess database.SessionRunner,
	kgRepo kgStore,
	workspaceID string,
	hall string,
	classification *extract.LLMClassification,
) (entityCount, tripleCount int) {
	if kgRepo == nil {
		return 0, 0
	}

	for _, entity := range classification.Entities {
		if _, err := kgRepo.AddEntity(ctx, sess, workspaceID, entity.Name, entity.Type, "{}"); err != nil {
			slog.WarnContext(ctx, "memory-jobs: add entity failed",
				slog.String("workspace_id", workspaceID),
				slog.String("entity", entity.Name),
				slog.String("error", err.Error()),
			)
			continue
		}
		entityCount++
	}

	for _, triple := range classification.Triples {
		t := &memory.Triple{
			WorkspaceID:  workspaceID,
			Subject:      triple.Subject,
			Predicate:    triple.Predicate,
			Object:       triple.Object,
			Confidence:   1.0,
			SourceCloset: hall,
		}
		if _, err := kgRepo.AddTriple(ctx, sess, workspaceID, t); err != nil {
			slog.WarnContext(ctx, "memory-jobs: add triple failed",
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
