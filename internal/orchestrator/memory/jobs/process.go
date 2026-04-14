package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

const (
	activeWorkspaceHours = 24
	rawDrawerBatchSize   = 50
	importanceScale      = 5.0
)

// ProcessDeps holds dependencies for the memory processing job.
// Repo fields are typed against consumer-side interfaces declared in
// ports.go.
type ProcessDeps struct {
	DB            *dbr.Connection
	DrawerRepo    drawerStore
	KGRepo        kgStore
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
		processWorkspace(ctx, sess, deps, wsID, result)
	}
	return result, nil
}

// processWorkspace runs the cold pipeline for every raw drawer in one workspace.
func processWorkspace(ctx context.Context, sess database.SessionRunner, deps ProcessDeps, wsID string, result *ProcessResult) {
	drawers, err := deps.DrawerRepo.ListByState(ctx, sess, wsID, string(memory.DrawerStateRaw), rawDrawerBatchSize)
	if err != nil {
		slog.Warn("memory-process: list raw drawers failed", "workspace_id", wsID, "error", err)
		return
	}
	if len(drawers) == 0 {
		return
	}

	classifications, batchErr := classifyBatch(ctx, deps.LLMClassifier, drawers)

	for i := range drawers {
		processSingleDrawer(ctx, sess, processSingleDrawerOpts{
			Deps:            deps,
			WorkspaceID:     wsID,
			Drawer:          &drawers[i],
			Classifications: classifications,
			Idx:             i,
			BatchErr:        batchErr,
			Result:          result,
		})
	}
}

// classifyBatch runs one batch LLM classify call for all drawers in a workspace.
// Callers fall back to per-drawer classification when a batch slot is missing.
func classifyBatch(ctx context.Context, classifier extract.LLMClassifier, drawers []memory.Drawer) ([]*extract.LLMClassification, error) {
	contents := make([]string, len(drawers))
	for i := range drawers {
		contents[i] = drawers[i].Content
	}
	batchCtx, cancel := context.WithTimeout(ctx, time.Duration(memory.ColdWorkerLLMTimeout)*time.Second)
	defer cancel()
	return classifier.ClassifyBatch(batchCtx, contents)
}

// processSingleDrawerOpts groups the per-drawer parameters for processSingleDrawer.
// ctx and sess remain positional per the project session/opts/repo pattern.
type processSingleDrawerOpts struct {
	Deps            ProcessDeps
	WorkspaceID     string
	Drawer          *memory.Drawer
	Classifications []*extract.LLMClassification
	Idx             int
	BatchErr        error
	Result          *ProcessResult
}

// processSingleDrawer applies one drawer's classification, falling back to
// a per-drawer LLM call when the batch entry is missing. Updates result
// counters in place.
func processSingleDrawer(ctx context.Context, sess database.SessionRunner, opts processSingleDrawerOpts) {
	deps := opts.Deps
	wsID := opts.WorkspaceID
	d := opts.Drawer
	classification, err := resolveClassification(ctx, deps.LLMClassifier, d, opts.Classifications, opts.Idx, opts.BatchErr)
	if err != nil {
		slog.Warn("memory-process: drawer classify failed",
			"drawer_id", d.ID, "workspace_id", wsID, "retry_count", d.RetryCount, "error", err)
		handleProcessFailure(ctx, sess, deps.DrawerRepo, d)
		opts.Result.Failed++
		return
	}

	if err := applyClassification(ctx, sess, deps, d, classification); err != nil {
		slog.Warn("memory-process: drawer failed",
			"drawer_id", d.ID, "workspace_id", wsID, "retry_count", d.RetryCount, "error", err)
		handleProcessFailure(ctx, sess, deps.DrawerRepo, d)
		opts.Result.Failed++
		return
	}
	opts.Result.Processed++
}

// resolveClassification returns the batch classification for idx, falling
// back to a per-drawer LLM call when the batch failed entirely or the slot
// is missing.
func resolveClassification(
	ctx context.Context,
	classifier extract.LLMClassifier,
	d *memory.Drawer,
	classifications []*extract.LLMClassification,
	idx int,
	batchErr error,
) (*extract.LLMClassification, error) {
	if batchErr == nil && idx < len(classifications) && classifications[idx] != nil {
		return classifications[idx], nil
	}
	slog.Warn("memory-process: batch classify unavailable, falling back",
		"drawer_id", d.ID, "workspace_id", d.WorkspaceID, "error", batchErr)
	singleCtx, cancel := context.WithTimeout(ctx, time.Duration(memory.ColdWorkerLLMTimeout)*time.Second)
	defer cancel()
	return classifier.ClassifyAndExtract(singleCtx, d.Content)
}

// applyClassification persists a classification result and runs downstream steps
// (entity linking, clustering, conflict detection) for a single drawer.
func applyClassification(ctx context.Context, sess database.SessionRunner, deps ProcessDeps, d *memory.Drawer, classification *extract.LLMClassification) error {
	scaledImportance := classification.Importance * importanceScale
	room := memory.MemoryTypeToRoom(classification.MemoryType)

	if err := deps.DrawerRepo.UpdateClassification(ctx, sess, drawerrepo.UpdateClassificationOpts{
		WorkspaceID: d.WorkspaceID,
		DrawerID:    d.ID,
		MemoryType:  classification.MemoryType,
		Summary:     classification.Summary,
		Room:        room,
		Importance:  scaledImportance,
	}); err != nil {
		return fmt.Errorf("update classification: %w", err)
	}

	// Sync the in-memory struct so downstream helpers (clusterDrawers,
	// detectDrawerConflicts) see fresh values instead of the raw pre-classification state.
	d.MemoryType = classification.MemoryType
	d.Summary = classification.Summary
	d.Room = room
	d.Importance = scaledImportance

	entityCount, tripleCount := linkAndCount(ctx, sess, deps.KGRepo, d.WorkspaceID, d.Hall, classification)
	if err := deps.DrawerRepo.UpdateEnrichment(ctx, sess, d.WorkspaceID, d.ID, entityCount, tripleCount); err != nil {
		slog.Warn("memory-process: update enrichment counts failed",
			"drawer_id", d.ID, "workspace_id", d.WorkspaceID, "error", err)
	}

	// Embed once for clustering and conflict detection, then persist so the
	// drawer is visible to vector search (#62).
	var embedding []float32
	if deps.Embedder != nil {
		var embedErr error
		embedding, embedErr = deps.Embedder.Embed(ctx, d.Content)
		if embedErr != nil {
			slog.Warn("memory-process: embed failed", "drawer_id", d.ID, "error", embedErr)
		} else if len(embedding) > 0 {
			if embedErr = deps.DrawerRepo.UpdateEmbedding(ctx, sess, d.WorkspaceID, d.ID, embedding); embedErr != nil {
				slog.Warn("memory-process: persist embedding failed", "drawer_id", d.ID, "error", embedErr)
			}
		}
	}

	clusterDrawers(ctx, sess, deps, d, embedding)
	detectDrawerConflicts(ctx, sess, deps, d, embedding)
	return deps.DrawerRepo.UpdateState(ctx, sess, d.WorkspaceID, d.ID, string(memory.DrawerStateProcessed))
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
		mergeClusterMember(ctx, sess, deps, d, &cluster[i])
	}
	if err := deps.DrawerRepo.UpdateClassification(ctx, sess, drawerrepo.UpdateClassificationOpts{
		WorkspaceID: d.WorkspaceID,
		DrawerID:    d.ID,
		MemoryType:  d.MemoryType,
		Summary:     mergedSummary,
		Room:        d.Room,
		Importance:  d.Importance,
	}); err != nil {
		slog.Warn("memory-process: update cluster leader classification failed",
			"drawer_id", d.ID, "workspace_id", d.WorkspaceID, "error", err)
	}
}

// mergeClusterMember attaches a similar drawer to the cluster leader and
// marks it merged. Errors on either step are logged and swallowed — the
// member stays where it was and the next sweep will retry.
func mergeClusterMember(ctx context.Context, sess database.SessionRunner, deps ProcessDeps, leader *memory.Drawer, member *memory.DrawerSearchResult) {
	if err := deps.DrawerRepo.SetClusterID(ctx, sess, leader.WorkspaceID, member.ID, leader.ID); err != nil {
		slog.Warn("memory-process: set cluster id failed",
			"member_id", member.ID, "leader_id", leader.ID, "error", err)
		return
	}
	if err := deps.DrawerRepo.UpdateState(ctx, sess, leader.WorkspaceID, member.ID, string(memory.DrawerStateMerged)); err != nil {
		slog.Warn("memory-process: mark cluster member merged failed",
			"member_id", member.ID, "leader_id", leader.ID, "error", err)
	}
}

func detectDrawerConflicts(ctx context.Context, sess database.SessionRunner, deps ProcessDeps, d *memory.Drawer, embedding []float32) {
	if len(embedding) == 0 || deps.LLMClassifier == nil {
		return
	}
	neighbors, err := deps.DrawerRepo.Search(ctx, sess, d.WorkspaceID, embedding, "", "", 5)
	if err != nil {
		slog.Warn("memory-process: neighbor search for conflict detection failed",
			"drawer_id", d.ID, "workspace_id", d.WorkspaceID, "error", err)
		return
	}
	for i := range neighbors {
		resolveNeighborConflict(ctx, sess, deps, d, &neighbors[i])
	}
}

// resolveNeighborConflict evaluates one neighbor drawer for a potential
// supersede relationship. Any LLM or repo error is logged and the
// relationship is left untouched so a later sweep can retry.
func resolveNeighborConflict(ctx context.Context, sess database.SessionRunner, deps ProcessDeps, d *memory.Drawer, neighbor *memory.DrawerSearchResult) {
	if neighbor.ID == d.ID {
		return
	}
	if neighbor.Similarity < memory.ColdWorkerConflictLow || neighbor.Similarity >= memory.ColdWorkerConflictHigh {
		return
	}
	conflicts, err := deps.LLMClassifier.DetectConflict(ctx, d.Content, neighbor.Content)
	if err != nil {
		slog.Warn("memory-process: detect conflict llm call failed",
			"new_drawer", d.ID, "old_drawer", neighbor.ID, "error", err)
		return
	}
	if !conflicts {
		return
	}
	slog.Info("memory-process: conflict detected",
		"new_drawer", d.ID, "old_drawer", neighbor.ID, "similarity", neighbor.Similarity)
	if err := deps.DrawerRepo.SetSupersededBy(ctx, sess, d.WorkspaceID, neighbor.ID, d.ID); err != nil {
		slog.Warn("memory-process: mark superseded failed",
			"new_drawer", d.ID, "old_drawer", neighbor.ID, "error", err)
	}
}

func handleProcessFailure(ctx context.Context, sess database.SessionRunner, drawerRepo drawerStore, d *memory.Drawer) {
	if err := drawerRepo.IncrementRetryCount(ctx, sess, d.WorkspaceID, d.ID); err != nil {
		slog.Warn("memory-process: increment retry count failed",
			"drawer_id", d.ID, "workspace_id", d.WorkspaceID, "error", err)
	}
	if d.RetryCount+1 < memory.ColdWorkerMaxRetries {
		return
	}
	slog.Warn("memory-process: max retries, marking failed",
		"drawer_id", d.ID, "workspace_id", d.WorkspaceID)
	if err := drawerRepo.UpdateState(ctx, sess, d.WorkspaceID, d.ID, string(memory.DrawerStateFailed)); err != nil {
		slog.Warn("memory-process: mark drawer failed state update failed",
			"drawer_id", d.ID, "workspace_id", d.WorkspaceID, "error", err)
	}
}
