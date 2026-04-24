package autoingest

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/alitto/pond/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/config"
)

// NewService constructs a Service backed by a pond.Pool. The pool is
// started eagerly; the caller must call Shutdown before the process exits.
// Returns an error if deps validation fails or the noise config cannot be
// loaded — the orchestrator can then log and exit cleanly without panicking.
func NewService(deps Deps, cfg Config) (Service, error) {
	if err := deps.Validate(); err != nil {
		return nil, fmt.Errorf("memory.autoingest: invalid deps: %w", err)
	}

	noiseCfg, err := config.LoadNoiseConfig()
	if err != nil {
		return nil, fmt.Errorf("memory.autoingest: load noise config: %w", err)
	}

	workers := cfg.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	pool := pond.NewPool(
		workers,
		pond.WithQueueSize(queueSize),
		pond.WithNonBlocking(true),
	)

	logger.Info("memory.autoingest: pool started",
		slog.Int("workers", workers),
		slog.Int("queue_size", queueSize),
	)

	return &service{
		pool:           pool,
		deps:           deps,
		logger:         logger,
		noiseMinLength: noiseCfg.MinLength,
		noisePattern:   noiseCfg.CompileNoisePattern(),
	}, nil
}

// Submit enqueues one Work for background ingestion. Deps.Validate in
// NewService guarantees DrawerRepo and Classifier are non-nil, so the
// hot path only filters noise and hands off to pond.
func (s *service) Submit(ctx context.Context, work Work) {
	if isNoise(work.Exchange, s.noiseMinLength, s.noisePattern) {
		return
	}
	// Capture a background context so long-lived ingestion does not
	// get cancelled when the request goroutine returns. chatservice
	// hands us a request-scoped ctx; we only use it to stamp the
	// logger / tracing.
	runCtx := context.WithoutCancel(ctx)
	_, ok := s.pool.TrySubmit(func() {
		s.runChunkPipeline(runCtx, work)
	})
	if !ok {
		s.dropped.Add(1)
		s.logger.WarnContext(ctx, "memory.autoingest: pool full, dropping work",
			slog.String("workspace_id", work.WorkspaceID),
			slog.String("agent", work.AgentSlug),
		)
	}
}

// Shutdown closes the pool and waits for in-flight tasks to finish.
// If the supplied ctx expires first, the remaining work is abandoned
// (workers observe cancellation via runCtx and return) and the context
// error is returned.
func (s *service) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.pool.StopAndWait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Metrics proxies pond counters plus the locally tracked drop and
// centroid-lookup-error counters. dropped and centroidErrors are kept
// locally because the pond queue "drop" counter counts only items
// rejected by the queue itself, and pond has no visibility into our
// per-step Phase 2 lookup failures.
func (s *service) Metrics() Metrics {
	return Metrics{
		Running:        s.pool.RunningWorkers(),
		Waiting:        s.pool.WaitingTasks(),
		Completed:      s.pool.CompletedTasks(),
		Dropped:        s.dropped.Load(),
		CentroidErrors: s.centroidErrors.Load(),
	}
}
