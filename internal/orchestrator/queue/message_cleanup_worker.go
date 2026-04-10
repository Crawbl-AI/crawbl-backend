package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/riverqueue/river"
	"github.com/robfig/cron/v3"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
)

// MessageCleanupQueue is the River queue name the stale-message cleanup
// worker runs on. Single-worker queue — there is no concurrency benefit
// to running multiple simultaneous sweeps.
const MessageCleanupQueue = "message_cleanup"

// pendingMessageMaxAge is how long a row can stay in status "pending"
// before we mark it as "failed". Five minutes is generous — the slowest
// LLM inference completes well under this window.
const pendingMessageMaxAge = 5 * time.Minute

// messageCleanupDedupeWindow collapses concurrent ad-hoc enqueues of the
// cleanup job inside a short window so we never double-scan. The periodic
// schedule fires once a minute anyway.
const messageCleanupDedupeWindow = 30 * time.Second

// MessageCleanupArgs is the empty River job payload for the periodic
// stale-pending-message cleanup. No per-job arguments — the worker always
// applies the same time cutoff to the whole messages table.
type MessageCleanupArgs struct{}

// Kind implements river.JobArgs.
func (MessageCleanupArgs) Kind() string { return "message_cleanup" }

// InsertOpts routes cleanup jobs onto their own queue and dedupes
// overlapping enqueues inside a 30-second window.
func (MessageCleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: MessageCleanupQueue,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: messageCleanupDedupeWindow,
		},
	}
}

// MessageCleanupDeps bundles the repo and logger the cleanup worker
// needs. Kept small because the job only touches one table.
type MessageCleanupDeps struct {
	DB          *dbr.Connection
	MessageRepo orchestratorrepo.MessageRepo
	Logger      *slog.Logger
}

// MessageCleanup is the River worker that marks stale pending messages
// as failed. It replaces the previous chatservice.StartPendingMessageCleanup
// ticker goroutine.
type MessageCleanup struct {
	river.WorkerDefaults[MessageCleanupArgs]
	deps MessageCleanupDeps
}

// NewMessageCleanup constructs a worker bound to the given dependencies.
func NewMessageCleanup(deps MessageCleanupDeps) *MessageCleanup {
	return &MessageCleanup{deps: deps}
}

// Work runs one sweep: find messages stuck in "pending" older than the
// max age and transition them to "failed".
func (w *MessageCleanup) Work(ctx context.Context, job *river.Job[MessageCleanupArgs]) error {
	if w.deps.MessageRepo == nil || w.deps.DB == nil {
		return nil
	}
	sess := w.deps.DB.NewSession(nil)
	cutoff := time.Now().UTC().Add(-pendingMessageMaxAge)

	count, mErr := w.deps.MessageRepo.FailStalePending(ctx, sess, cutoff)
	if mErr != nil {
		w.deps.Logger.WarnContext(ctx, "message-cleanup: fail stale pending failed",
			slog.Int64("job_id", job.ID),
			slog.String("error", mErr.Error()),
		)
		return nil
	}
	if count > 0 {
		w.deps.Logger.InfoContext(ctx, "message-cleanup: marked stale pending messages",
			slog.Int64("job_id", job.ID),
			slog.Int("count", count),
		)
	}
	return nil
}

// RegisterMessageCleanup wires the cleanup worker, queue, and 1-minute
// periodic schedule onto an existing River workers registry. Call this
// after background.NewConfig so the cleanup shares the same river.Client.
func RegisterMessageCleanup(
	workers *river.Workers,
	queues map[string]river.QueueConfig,
	periodic *[]*river.PeriodicJob,
	deps MessageCleanupDeps,
) error {
	river.AddWorker(workers, NewMessageCleanup(deps))
	queues[MessageCleanupQueue] = river.QueueConfig{MaxWorkers: 1}

	schedule, err := cron.ParseStandard("* * * * *")
	if err != nil {
		return fmt.Errorf("parse message cleanup schedule: %w", err)
	}
	*periodic = append(*periodic, river.NewPeriodicJob(
		schedule,
		func() (river.JobArgs, *river.InsertOpts) {
			return MessageCleanupArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: false},
	))
	return nil
}
