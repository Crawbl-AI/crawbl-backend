package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/drawer"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/kg"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

const (
	activeWorkspaceHours = 24
	rawDrawerBatchSize   = 50
	importanceScale      = 5.0
)

// ProcessDeps holds dependencies for the memory processing job.
type ProcessDeps struct {
	DB            *dbr.Connection
	DrawerRepo    drawer.Repo
	KGGraph       kg.Graph
	LLMClassifier extract.LLMClassifier
	Embedder      embed.Embedder
}

// ProcessResult holds the outcome of a processing run.
type ProcessResult struct {
	Processed int
	Failed    int
}

// RunProcess fetches all raw drawers and processes them through the cold pipeline.
func RunProcess(ctx context.Context, deps ProcessDeps) (*ProcessResult, error) {
	sess := deps.DB.NewSession(nil)

	// Get active workspaces with raw drawers.
	activeIDs, err := deps.DrawerRepo.ActiveWorkspaces(ctx, sess, activeWorkspaceHours)
	if err != nil {
		return nil, fmt.Errorf("list active workspaces: %w", err)
	}

	if len(activeIDs) == 0 {
		slog.Info("memory-process: no active workspaces")
		return &ProcessResult{}, nil
	}

	result := &ProcessResult{}
	for _, wsID := range activeIDs {
		drawers, err := deps.DrawerRepo.ListByState(ctx, sess, wsID, string(memory.DrawerStateRaw), rawDrawerBatchSize)
		if err != nil {
			slog.Warn("memory-process: list raw drawers failed", "workspace_id", wsID, "error", err)
			continue
		}

		for i := range drawers {
			d := &drawers[i]
			if err := processOneDrawer(ctx, sess, deps, d); err != nil {
				slog.Warn("memory-process: drawer failed",
					"drawer_id", d.ID, "workspace_id", d.WorkspaceID,
					"retry_count", d.RetryCount, "error", err)
				handleProcessFailure(ctx, sess, deps.DrawerRepo, d)
				result.Failed++
				continue
			}
			result.Processed++
		}
	}

	return result, nil
}

func processOneDrawer(ctx context.Context, sess database.SessionRunner, deps ProcessDeps, d *memory.Drawer) error {
	classifyCtx, cancel := context.WithTimeout(ctx, time.Duration(memory.ColdWorkerLLMTimeout)*time.Second)
	defer cancel()

	classification, err := deps.LLMClassifier.ClassifyAndExtract(classifyCtx, d.Content)
	if err != nil {
		return fmt.Errorf("classify: %w", err)
	}

	scaledImportance := classification.Importance * importanceScale
	room := memory.MemoryTypeToRoom(classification.MemoryType)

	if err := deps.DrawerRepo.UpdateClassification(ctx, sess, d.ID,
		classification.MemoryType, classification.Summary, room, scaledImportance,
	); err != nil {
		return fmt.Errorf("update classification: %w", err)
	}

	linkEntities(ctx, sess, deps, d.WorkspaceID, classification)

	// Embed once for clustering and conflict detection.
	var embedding []float32
	if deps.Embedder != nil {
		var embedErr error
		embedding, embedErr = deps.Embedder.Embed(ctx, d.Content)
		if embedErr != nil {
			slog.Warn("memory-process: embed failed", "drawer_id", d.ID, "error", embedErr)
		}
	}

	clusterDrawers(ctx, sess, deps, d, embedding)
	detectDrawerConflicts(ctx, sess, deps, d, embedding)
	return deps.DrawerRepo.UpdateState(ctx, sess, d.ID, string(memory.DrawerStateProcessed))
}

func linkEntities(ctx context.Context, sess database.SessionRunner, deps ProcessDeps, workspaceID string, classification *extract.LLMClassification) {
	if deps.KGGraph == nil {
		return
	}
	for _, entity := range classification.Entities {
		if _, err := deps.KGGraph.AddEntity(ctx, sess, workspaceID, entity.Name, entity.Type, "{}"); err != nil {
			slog.Warn("memory-process: add entity failed", "entity", entity.Name, "error", err)
		}
	}
	for _, triple := range classification.Triples {
		t := &memory.Triple{
			WorkspaceID: workspaceID,
			Subject:     triple.Subject,
			Predicate:   triple.Predicate,
			Object:      triple.Object,
			Confidence:  1.0,
		}
		if _, err := deps.KGGraph.AddTriple(ctx, sess, workspaceID, t); err != nil {
			slog.Warn("memory-process: add triple failed",
				"subject", triple.Subject, "predicate", triple.Predicate, "error", err)
		}
	}
}

func clusterDrawers(ctx context.Context, sess database.SessionRunner, deps ProcessDeps, d *memory.Drawer, embedding []float32) {
	if len(embedding) == 0 {
		return
	}
	similar, err := deps.DrawerRepo.Search(ctx, sess, d.WorkspaceID, embedding, "", "", 10)
	if err != nil {
		return
	}
	var cluster []memory.DrawerSearchResult
	for i := range similar {
		sr := &similar[i]
		if sr.Similarity >= memory.ColdWorkerClusterThreshold && sr.ID != d.ID && sr.State == string(memory.DrawerStateProcessed) {
			cluster = append(cluster, *sr)
		}
	}
	if len(cluster) < 2 {
		return
	}
	contents := make([]string, 0, len(cluster)+1)
	contents = append(contents, d.Content)
	for i := range cluster {
		contents = append(contents, cluster[i].Content)
	}
	mergedSummary, err := deps.LLMClassifier.MergeSummary(ctx, contents)
	if err != nil {
		slog.Warn("memory-process: merge summary failed", "error", err)
		return
	}
	for i := range cluster {
		member := &cluster[i]
		_ = deps.DrawerRepo.SetClusterID(ctx, sess, member.ID, d.ID)
		_ = deps.DrawerRepo.UpdateState(ctx, sess, member.ID, string(memory.DrawerStateMerged))
	}
	_ = deps.DrawerRepo.UpdateClassification(ctx, sess, d.ID, d.MemoryType, mergedSummary, d.Room, d.Importance)
}

func detectDrawerConflicts(ctx context.Context, sess database.SessionRunner, deps ProcessDeps, d *memory.Drawer, embedding []float32) {
	if len(embedding) == 0 || deps.LLMClassifier == nil {
		return
	}
	neighbors, err := deps.DrawerRepo.Search(ctx, sess, d.WorkspaceID, embedding, "", "", 5)
	if err != nil {
		return
	}
	for i := range neighbors {
		neighbor := &neighbors[i]
		if neighbor.ID == d.ID {
			continue
		}
		if neighbor.Similarity < memory.ColdWorkerConflictLow || neighbor.Similarity >= memory.ColdWorkerConflictHigh {
			continue
		}
		conflicts, err := deps.LLMClassifier.DetectConflict(ctx, d.Content, neighbor.Content)
		if err != nil {
			continue
		}
		if conflicts {
			slog.Info("memory-process: conflict detected",
				"new_drawer", d.ID, "old_drawer", neighbor.ID, "similarity", neighbor.Similarity)
			_ = deps.DrawerRepo.SetSupersededBy(ctx, sess, neighbor.ID, d.ID)
		}
	}
}

func handleProcessFailure(ctx context.Context, sess database.SessionRunner, drawerRepo drawer.Repo, d *memory.Drawer) {
	_ = drawerRepo.IncrementRetryCount(ctx, sess, d.ID)
	if d.RetryCount+1 >= memory.ColdWorkerMaxRetries {
		slog.Warn("memory-process: max retries, marking failed",
			"drawer_id", d.ID, "workspace_id", d.WorkspaceID)
		_ = drawerRepo.UpdateState(ctx, sess, d.ID, string(memory.DrawerStateFailed))
	}
}
