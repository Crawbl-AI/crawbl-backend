package chatservice

import (
	"context"
	"log/slog"
	"time"
)

// pendingMessageMaxAge is how long a message can stay in "pending" status
// before the cleanup marks it as "failed". 5 minutes is generous — even the
// slowest LLM inference completes well within this window.
const pendingMessageMaxAge = 5 * time.Minute

// cleanupInterval is how often the background cleaner runs.
const cleanupInterval = 1 * time.Minute

// StartPendingMessageCleanup launches a background goroutine that periodically
// marks stale pending messages as failed. Call this once at service startup.
// The goroutine stops when ctx is cancelled. It opens a fresh session from
// s.db for each tick so the cleanup is not coupled to any request session.
// The returned channel is closed when the goroutine exits, allowing the caller
// to wait for in-flight cleanup before closing the DB connection pool.
func (s *service) StartPendingMessageCleanup(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.cleanupPendingMessages(ctx)
			}
		}
	}()
	return done
}

// cleanupPendingMessages finds messages with status "pending" older than
// pendingMessageMaxAge and marks them as "failed".
func (s *service) cleanupPendingMessages(ctx context.Context) {
	sess := s.db.NewSession(nil)
	cutoff := time.Now().UTC().Add(-pendingMessageMaxAge)

	count, mErr := s.messageRepo.FailStalePending(ctx, sess, cutoff)
	if mErr != nil {
		slog.Warn("cleanup: failed to mark stale pending messages",
			"error", mErr.Error(),
		)
		return
	}

	if count > 0 {
		slog.Info("cleanup: marked stale pending messages as failed",
			"count", count,
		)
	}
}
