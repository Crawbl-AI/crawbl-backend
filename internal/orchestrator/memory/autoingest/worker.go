package autoingest

import (
	"context"
	"log/slog"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// runChunkPipeline is the entry point of one Work item as executed by
// the pond worker. It chunks the exchange and, for each chunk, runs the
// classify → embed → dedup → persist → publish pipeline. Errors are
// logged and swallowed — the pool never retries auto-ingest work and
// the raw messages row stays in Postgres for future replay tooling.
func (s *service) runChunkPipeline(ctx context.Context, work Work) {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(memory.AutoIngestTimeout)*time.Second)
	defer cancel()

	sess := s.deps.DB.NewSession(nil)
	chunks := chunkText(work.Exchange, memory.AutoIngestChunkSize, memory.AutoIngestChunkOverlap, memory.AutoIngestMinChunk)

	var ingested, skippedDedup int
	start := time.Now()
	for _, chunk := range chunks {
		if runCtx.Err() != nil {
			s.logger.WarnContext(ctx, "memory.autoingest: chunk loop cancelled",
				slog.String("workspace_id", work.WorkspaceID),
				slog.String("error", runCtx.Err().Error()),
			)
			break
		}
		if s.ingestChunk(runCtx, sess, work, chunk) {
			ingested++
			continue
		}
		skippedDedup++
	}

	s.logger.InfoContext(ctx, "memory.autoingest: complete",
		slog.String("workspace_id", work.WorkspaceID),
		slog.String("agent", work.AgentSlug),
		slog.Int("ingested", ingested),
		slog.Int("skipped_dedup", skippedDedup),
		slog.Duration("duration", time.Since(start)),
	)
}

// ingestChunk handles one chunk: classify, embed, dedup, persist, then
// publish downstream events. Returns true when a drawer was written and
// false when the chunk was skipped (dedup hit, insert error, noise).
//
// Phase 0 deliberately does NOT enqueue a follow-up memory_process job.
// The existing 1-minute periodic sweep in background/config.go picks up
// raw drawers within ~60s — removing the ad-hoc enqueue is what makes
// the chat-turn critical path write zero river_job rows.
func (s *service) ingestChunk(ctx context.Context, sess database.SessionRunner, work Work, chunk string) bool {
	memType, importance, confidence := s.classify(chunk)
	room := memory.MemoryTypeToRoom(memType)

	embedding := s.embedChunk(ctx, work.WorkspaceID, chunk)
	if s.isDuplicate(ctx, sess, work.WorkspaceID, embedding) {
		return false
	}

	tier, state := s.pickTier(ctx, sess, work.WorkspaceID, memType, confidence, embedding)
	d := buildDrawer(work, chunk, memType, room, importance, tier, state)
	if err := s.deps.DrawerRepo.AddIdempotent(ctx, sess, d, embedding); err != nil {
		s.logger.WarnContext(ctx, "memory.autoingest: drawer insert failed",
			slog.String("workspace_id", work.WorkspaceID),
			slog.String("drawer_id", d.ID),
			slog.String("error", err.Error()),
		)
		return false
	}

	s.logger.InfoContext(ctx, "memory.autoingest.tier",
		slog.String("workspace_id", work.WorkspaceID),
		slog.String("drawer_id", d.ID),
		slog.String("tier", tier),
		slog.Float64("confidence", confidence),
		slog.String("memory_type", memType),
	)

	s.publishMemoryEvent(ctx, work, d, len(chunk))
	return true
}

// pickTier decides the pipeline tier + drawer state for a chunk based
// on heuristic confidence and (optionally) the nearest centroid lookup.
// The caller threads the per-Work session so pickTier never opens its
// own — keeping the hot path to a single *dbr.Session per Work item.
//
//   - confidence >= HeuristicConfidenceHigh → PipelineTierHeuristic, state=processed
//   - confidence in [HeuristicConfidenceLow, HeuristicConfidenceHigh) AND
//     centroid lookup finds similarity > MemoryCentroidThreshold →
//     PipelineTierCentroid, state=processed
//   - otherwise → PipelineTierLLM, state=raw (cold-process picks it up)
//
// Phase 0: HeuristicConfidenceHigh/Low are set to 999 by default so every
// chunk falls to the LLM branch, identical to the pre-Phase-1 behavior.
// Phase 1 flips HeuristicConfidenceHigh to 0.8 via env var. Phase 2 wires
// in CentroidRepo.
func (s *service) pickTier(ctx context.Context, sess database.SessionRunner, workspaceID, memType string, confidence float64, embedding []float32) (string, string) {
	if confidence >= memory.HeuristicConfidenceHigh {
		return memory.PipelineTierHeuristic, string(memory.DrawerStateProcessed)
	}
	if confidence < memory.HeuristicConfidenceLow || s.deps.CentroidRepo == nil || len(embedding) == 0 {
		return memory.PipelineTierLLM, string(memory.DrawerStateRaw)
	}
	match, sim, ok, err := s.deps.CentroidRepo.NearestType(ctx, sess, workspaceID, embedding)
	if err != nil {
		s.centroidErrors.Add(1)
		s.logger.WarnContext(ctx, "memory.autoingest.centroid_lookup_failed",
			slog.String("workspace_id", workspaceID),
			slog.String("error", err.Error()),
		)
		return memory.PipelineTierLLM, string(memory.DrawerStateRaw)
	}
	if !ok || sim < memory.MemoryCentroidThreshold {
		return memory.PipelineTierLLM, string(memory.DrawerStateRaw)
	}
	s.logger.InfoContext(ctx, "memory.autoingest.centroid_prediction",
		slog.String("workspace_id", workspaceID),
		slog.Float64("similarity", sim),
		slog.String("heuristic_type", memType),
		slog.String("centroid_type", match),
	)
	return memory.PipelineTierCentroid, string(memory.DrawerStateProcessed)
}

// classify runs the heuristic classifier on a chunk and returns the
// memory type, scaled importance, and the raw classifier confidence.
// Deps.Validate guarantees a non-nil classifier at construction so no
// nil guard is required here.
func (s *service) classify(chunk string) (string, float64, float64) {
	classified := s.deps.Classifier.Classify(chunk, memory.AutoIngestMinConfidence)
	if len(classified) == 0 {
		return "", memory.DefaultImportance, 0
	}
	return classified[0].MemoryType, classified[0].Confidence * importanceScale, classified[0].Confidence
}

// embedChunk computes the embedding for a chunk, logging (but not
// propagating) any embed error so the caller can still persist the
// drawer without vector dedup.
func (s *service) embedChunk(ctx context.Context, workspaceID, chunk string) []float32 {
	if s.deps.Embedder == nil {
		return nil
	}
	embedding, err := s.deps.Embedder.Embed(ctx, chunk)
	if err != nil {
		s.logger.WarnContext(ctx, "memory.autoingest: embedding failed",
			slog.String("workspace_id", workspaceID),
			slog.String("error", err.Error()),
		)
		return nil
	}
	return embedding
}

// isDuplicate returns true when the chunk's embedding already has a
// near-identical drawer in the workspace. A nil embedding always returns
// false so we still persist chunks when the embedder is offline.
func (s *service) isDuplicate(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32) bool {
	if len(embedding) == 0 {
		return false
	}
	dupes, err := s.deps.DrawerRepo.CheckDuplicate(ctx, sess, workspaceID, embedding, memory.AutoIngestDupThreshold, 1)
	if err != nil {
		s.logger.WarnContext(ctx, "memory.autoingest: dedup lookup failed",
			slog.String("workspace_id", workspaceID),
			slog.String("error", err.Error()),
		)
		return false
	}
	return len(dupes) > 0
}

// publishMemoryEvent fans out a MemoryEvent via the publisher bundled
// into deps. No-op when no publisher is configured.
func (s *service) publishMemoryEvent(ctx context.Context, work Work, d *memory.Drawer, contentLen int) {
	if s.deps.MemoryPublisher == nil {
		return
	}
	s.deps.MemoryPublisher.Publish(ctx, work.WorkspaceID, &queue.MemoryEvent{
		WorkspaceID: work.WorkspaceID,
		DrawerID:    d.ID,
		Wing:        d.Wing,
		Room:        d.Room,
		MemoryType:  d.MemoryType,
		AgentID:     work.AgentSlug,
		ContentLen:  contentLen,
	})
}
