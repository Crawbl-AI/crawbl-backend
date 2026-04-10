package queue

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/llmusagerepo"
)

// UsageWriteQueue is the River queue name usage_write jobs run on.
const UsageWriteQueue = "usage_write"

// usageWorkerConcurrency caps concurrent ClickHouse writes. ClickHouse
// buffers async inserts server-side, so a low client-side concurrency
// is enough to keep throughput up without hammering connections.
const usageWorkerConcurrency = 4

// Kind implements river.JobArgs on the shared UsageEvent type, so the
// same struct serves as both the publisher payload and the River job
// args — no duplicate field lists, no copy step at enqueue time.
func (UsageEvent) Kind() string { return "usage_write" }

// InsertOpts routes every usage_write job onto the usage_write queue.
// Uniqueness is NOT enforced at the River layer: each event has its
// own deterministic event_id and ClickHouse deduplicates on primary
// key, so accidental double-enqueues collapse at the database layer.
func (UsageEvent) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: UsageWriteQueue}
}

// UsageWriterDeps bundles the infrastructure the usage_write worker
// needs. The repo is the only collaborator — the queue package stays
// free of raw SQL, database/sql imports, and connection management.
type UsageWriterDeps struct {
	Repo   llmusagerepo.Repo
	Logger *slog.Logger
}

// UsageWriter is the River worker that persists a single usage event
// via llmusagerepo. Thin adapter: build the repo-layer event, delegate
// the write, return any error so River handles retries/backoff.
type UsageWriter struct {
	river.WorkerDefaults[UsageEvent]
	deps UsageWriterDeps
}

// NewUsageWriter constructs a worker bound to the given dependencies.
func NewUsageWriter(deps UsageWriterDeps) *UsageWriter {
	return &UsageWriter{deps: deps}
}

// Work runs one llmusagerepo.Insert for this job's event. Errors are
// returned unwrapped so River's exponential backoff retries.
func (w *UsageWriter) Work(ctx context.Context, job *river.Job[UsageEvent]) error {
	if w.deps.Repo == nil {
		// Missing repo = analytics disabled; silent no-op.
		return nil
	}

	e := job.Args
	if err := w.deps.Repo.Insert(ctx, &llmusagerepo.LLMUsageEvent{
		EventID:             e.EventID,
		EventTime:           e.EventTime,
		UserID:              e.UserID,
		WorkspaceID:         e.WorkspaceID,
		ConversationID:      e.ConversationID,
		MessageID:           e.MessageID,
		AgentID:             e.AgentID,
		AgentDBID:           e.AgentDBID,
		Model:               e.Model,
		Provider:            e.Provider,
		PromptTokens:        e.PromptTokens,
		CompletionTokens:    e.CompletionTokens,
		TotalTokens:         e.TotalTokens,
		ToolUsePromptTokens: e.ToolUsePromptTokens,
		ThoughtsTokens:      e.ThoughtsTokens,
		CachedTokens:        e.CachedTokens,
		CostUSD:             e.CostUSD,
		CallSequence:        e.CallSequence,
		TurnID:              e.TurnID,
		SessionID:           e.SessionID,
	}); err != nil {
		w.deps.Logger.Error("usage_write job failed",
			slog.Int64("job_id", job.ID),
			slog.String("event_id", e.EventID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("llm usage repo insert: %w", err)
	}
	return nil
}

// RegisterUsageWriter wires the usage_write worker and queue onto an
// existing River workers registry and queues map. Call this after
// background.NewConfig() so memory and usage workers share one
// river.Client.
func RegisterUsageWriter(workers *river.Workers, queues map[string]river.QueueConfig, deps UsageWriterDeps) {
	river.AddWorker(workers, NewUsageWriter(deps))
	queues[UsageWriteQueue] = river.QueueConfig{MaxWorkers: usageWorkerConcurrency}
}
