package background

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/config"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
)

// QueueMemoryAutoIngest is the River queue name for auto-ingest jobs —
// one per conversation exchange that has to be chunked, classified, and
// stored as raw drawers.
const QueueMemoryAutoIngest = "memory_autoingest"

// autoIngestConcurrency bounds concurrent auto-ingest workers. The
// pipeline is embedding-bound (one HTTP call per chunk), so a handful of
// workers is plenty until we have real load data.
const autoIngestConcurrency = 4

// importanceScale turns the classifier's 0..1 confidence into the
// memory_drawers.importance scale used by the ranking pipeline.
const importanceScale = 5.0

// noiseConfig holds the loaded noise-filter pattern at package init so we
// never pay the regex compile cost per job.
var noiseConfig = mustLoadNoiseConfig()
var noisePattern = noiseConfig.CompileNoisePattern()

// sentenceBoundary splits text on sentence-ending punctuation followed by
// whitespace. Used by chunkText to respect natural break points.
var sentenceBoundary = regexp.MustCompile(`([.!?])\s+`)

func mustLoadNoiseConfig() *config.NoiseConfig {
	cfg, err := config.LoadNoiseConfig()
	if err != nil {
		panic(fmt.Sprintf("failed to load noise config: %v", err))
	}
	return cfg
}

// AutoIngestArgs is the River job payload for one conversation exchange
// that needs to be broken down into raw memory drawers. Exchange is the
// already-joined "User: ...\n\nAgent: ..." string produced at request
// time — the worker owns chunking, classification, and persistence.
type AutoIngestArgs struct {
	WorkspaceID string `json:"workspace_id"`
	AgentSlug   string `json:"agent_slug"`
	Exchange    string `json:"exchange"`
}

// Kind implements river.JobArgs.
func (AutoIngestArgs) Kind() string { return "memory_autoingest" }

// InsertOpts routes auto-ingest onto its own queue. We do NOT dedupe by
// args here — the same workspace/agent legitimately produces multiple
// exchanges per minute and every one should run.
func (AutoIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMemoryAutoIngest}
}

// AutoIngestWorker is the River worker that drains AutoIngestArgs jobs.
// It replaces the old chatservice in-process goroutine + channel queue.
type AutoIngestWorker struct {
	river.WorkerDefaults[AutoIngestArgs]
	deps Deps
}

// NewAutoIngestWorker constructs a worker bound to the given dependencies.
func NewAutoIngestWorker(deps Deps) *AutoIngestWorker {
	return &AutoIngestWorker{deps: deps}
}

// Work executes one auto-ingest job. Errors are wrapped so River's
// backoff handles retries automatically.
func (w *AutoIngestWorker) Work(ctx context.Context, job *river.Job[AutoIngestArgs]) error {
	if w.deps.DrawerRepo == nil {
		return nil
	}
	if isNoise(job.Args.Exchange) {
		return nil
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(memory.AutoIngestTimeout)*time.Second)
	defer cancel()

	sess := w.deps.DB.NewSession(nil)
	chunks := chunkText(job.Args.Exchange, memory.AutoIngestChunkSize, memory.AutoIngestChunkOverlap, memory.AutoIngestMinChunk)

	var ingested, skippedDedup int
	start := time.Now()
	for _, chunk := range chunks {
		if w.ingestChunk(runCtx, sess, job.Args, chunk) {
			ingested++
			continue
		}
		skippedDedup++
	}

	w.deps.Logger.InfoContext(ctx, "auto-ingest complete",
		slog.String("workspace_id", job.Args.WorkspaceID),
		slog.String("agent", job.Args.AgentSlug),
		slog.Int("ingested", ingested),
		slog.Int("skipped_dedup", skippedDedup),
		slog.Duration("duration", time.Since(start)),
	)
	return nil
}

// ingestChunk handles one chunk: classify, embed, dedup, persist, then
// publish downstream events. Returns true when a drawer was written and
// false when the chunk was skipped (dedup hit or insert error).
func (w *AutoIngestWorker) ingestChunk(ctx context.Context, sess *dbr.Session, args AutoIngestArgs, chunk string) bool {
	memType, importance := w.classify(chunk)
	room := memory.MemoryTypeToRoom(memType)

	embedding := w.embedChunk(ctx, args.WorkspaceID, chunk)
	if w.isDuplicate(ctx, sess, args.WorkspaceID, embedding) {
		return false
	}

	d := buildDrawer(args, chunk, memType, room, importance)
	if err := w.deps.DrawerRepo.AddIdempotent(ctx, sess, d, embedding); err != nil {
		w.deps.Logger.WarnContext(ctx, "auto-ingest: drawer insert failed",
			slog.String("workspace_id", args.WorkspaceID),
			slog.String("drawer_id", d.ID),
			slog.String("error", err.Error()),
		)
		return false
	}

	w.publishMemoryEvent(ctx, args, d, len(chunk))
	w.enqueueProcessJob(ctx, args.WorkspaceID, d.ID)
	return true
}

// classify runs the heuristic classifier on a chunk and returns the
// memory type plus a scaled importance. A nil classifier returns the
// default importance and empty memory type.
func (w *AutoIngestWorker) classify(chunk string) (string, float64) {
	if w.deps.Classifier == nil {
		return "", memory.DefaultImportance
	}
	classified := w.deps.Classifier.Classify(chunk, memory.AutoIngestMinConfidence)
	if len(classified) == 0 {
		return "", memory.DefaultImportance
	}
	return classified[0].MemoryType, classified[0].Confidence * importanceScale
}

// embedChunk computes the embedding for a chunk, logging (but not
// propagating) any embed error so the caller can still persist the
// drawer without vector dedup.
func (w *AutoIngestWorker) embedChunk(ctx context.Context, workspaceID, chunk string) []float32 {
	if w.deps.Embedder == nil {
		return nil
	}
	embedding, err := w.deps.Embedder.Embed(ctx, chunk)
	if err != nil {
		w.deps.Logger.WarnContext(ctx, "auto-ingest: embedding failed",
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
func (w *AutoIngestWorker) isDuplicate(ctx context.Context, sess *dbr.Session, workspaceID string, embedding []float32) bool {
	if len(embedding) == 0 {
		return false
	}
	dupes, err := w.deps.DrawerRepo.CheckDuplicate(ctx, sess, workspaceID, embedding, memory.AutoIngestDupThreshold, 1)
	if err != nil {
		w.deps.Logger.WarnContext(ctx, "auto-ingest: dedup lookup failed",
			slog.String("workspace_id", workspaceID),
			slog.String("error", err.Error()),
		)
		return false
	}
	return len(dupes) > 0
}

// publishMemoryEvent fans out a MemoryEvent via the publisher bundled
// into deps. No-op when no publisher is configured.
func (w *AutoIngestWorker) publishMemoryEvent(ctx context.Context, args AutoIngestArgs, d *memory.Drawer, contentLen int) {
	if w.deps.MemoryPublisher == nil {
		return
	}
	w.deps.MemoryPublisher.Publish(ctx, args.WorkspaceID, &queue.MemoryEvent{
		WorkspaceID: args.WorkspaceID,
		DrawerID:    d.ID,
		Wing:        d.Wing,
		Room:        d.Room,
		MemoryType:  d.MemoryType,
		AgentID:     args.AgentSlug,
		ContentLen:  contentLen,
	})
}

// enqueueProcessJob fires an ad-hoc memory_process job so the cold
// pipeline runs within ~100ms instead of waiting for the next periodic
// sweep. Pulled from the River client in the worker's context so we do
// not need to hold a reference on Deps.
func (w *AutoIngestWorker) enqueueProcessJob(ctx context.Context, workspaceID, drawerID string) {
	client, err := river.ClientFromContextSafely[*sql.Tx](ctx)
	if err != nil || client == nil {
		return
	}
	if _, err := client.Insert(ctx, ProcessArgs{}, nil); err != nil {
		w.deps.Logger.WarnContext(ctx, "auto-ingest: river enqueue failed",
			slog.String("workspace_id", workspaceID),
			slog.String("drawer_id", drawerID),
			slog.String("error", err.Error()),
		)
	}
}

// buildDrawer assembles the memory.Drawer row we are about to insert.
// Kept as a plain function so worker methods above stay short.
func buildDrawer(args AutoIngestArgs, chunk, memType, room string, importance float64) *memory.Drawer {
	now := time.Now().UTC()
	return &memory.Drawer{
		ID:           autoIngestDrawerID(room, chunk),
		WorkspaceID:  args.WorkspaceID,
		Wing:         memory.AutoIngestWing,
		Room:         room,
		Content:      chunk,
		Importance:   importance,
		MemoryType:   memType,
		AddedBy:      memory.AutoIngestAddedBy,
		AddedByAgent: args.AgentSlug,
		State:        string(memory.DrawerStateRaw),
		FiledAt:      now,
		CreatedAt:    now,
	}
}

// isNoise reports whether the given exchange is too short or matches a
// greeting/filler pattern and should be dropped before chunking.
func isNoise(text string) bool {
	if len(text) < noiseConfig.MinLength {
		return true
	}
	return noisePattern.MatchString(strings.TrimSpace(text))
}

// chunkText splits text on sentence boundaries into chunks of at most
// maxSize characters with a trailing overlap carried into the next chunk.
// Chunks smaller than minChunk are discarded. If no sentence boundary is
// found the function falls back to a hard split.
func chunkText(text string, maxSize, overlap, minChunk int) []string {
	if len(text) <= maxSize {
		return []string{text}
	}

	sentences := splitSentences(text)
	chunks := assembleChunks(sentences, maxSize, overlap, minChunk)
	if len(chunks) > 0 {
		return chunks
	}
	return hardSplit(text, maxSize, overlap, minChunk)
}

// splitSentences returns the list of sentences in text, re-attaching each
// trailing punctuation character to the sentence it closed.
func splitSentences(text string) []string {
	parts := sentenceBoundary.Split(text, -1)
	puncts := sentenceBoundary.FindAllStringSubmatch(text, -1)
	sentences := make([]string, 0, len(parts))
	for i, part := range parts {
		if i < len(puncts) {
			sentences = append(sentences, part+puncts[i][1])
			continue
		}
		sentences = append(sentences, part)
	}
	return sentences
}

// assembleChunks walks the sentence stream and groups sentences into
// chunks bounded by maxSize, copying the trailing overlap into the next
// chunk's prefix so context is not lost at the boundary.
func assembleChunks(sentences []string, maxSize, overlap, minChunk int) []string {
	var chunks []string
	var current strings.Builder
	for _, sentence := range sentences {
		if current.Len() > 0 && current.Len()+1+len(sentence) > maxSize {
			chunk := current.String()
			if len(chunk) >= minChunk {
				chunks = append(chunks, chunk)
			}
			current.Reset()
			current.WriteString(tailOverlap(chunk, overlap))
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(sentence)
	}
	if current.Len() >= minChunk {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// tailOverlap returns the last `overlap` characters of chunk, or the
// whole chunk when it is shorter than the overlap window.
func tailOverlap(chunk string, overlap int) string {
	if overlap <= 0 {
		return ""
	}
	if len(chunk) > overlap {
		return chunk[len(chunk)-overlap:]
	}
	return chunk
}

// hardSplit is the fallback chunker for text without sentence boundaries.
// It walks the input in maxSize-overlap strides, preserving the overlap
// window at each boundary so downstream embedding keeps context.
func hardSplit(text string, maxSize, overlap, minChunk int) []string {
	var chunks []string
	for i := 0; i < len(text); i += maxSize - overlap {
		end := i + maxSize
		if end > len(text) {
			end = len(text)
		}
		if chunk := text[i:end]; len(chunk) >= minChunk {
			chunks = append(chunks, chunk)
		}
		if end == len(text) {
			break
		}
	}
	return chunks
}

// autoIngestDrawerID returns a deterministic drawer ID so repeated
// ingests of the same content are idempotent at the row level.
func autoIngestDrawerID(room, content string) string {
	hash := md5.Sum([]byte(content))
	return fmt.Sprintf("drawer_%s_%s_%x", memory.AutoIngestWing, room, hash[:8])
}
