package queue

import (
	"context"
	"log/slog"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/crawblnats"
	pkgriver "github.com/Crawbl-AI/crawbl-backend/internal/pkg/river"
)

// -----------------------------------------------------------------------------
// MemoryPublisher — publishes MemoryEvent onto NATS for downstream
// distillation / analytics consumers.
// -----------------------------------------------------------------------------

// MemoryPublisher publishes raw memory drawer events to NATS.
// Construction is nil-safe: a nil NATS client makes Publish a no-op,
// which lets the orchestrator boot in environments without NATS
// (local dev, CI).
type MemoryPublisher struct {
	nats   *crawblnats.Client
	logger *slog.Logger
}

// NewMemoryPublisher wires the NATS client and logger. Either may be
// nil: callers get a working (no-op) publisher back either way.
func NewMemoryPublisher(natsClient *crawblnats.Client, logger *slog.Logger) *MemoryPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryPublisher{nats: natsClient, logger: logger}
}

// Publish stamps the event with an ID and timestamp if the caller left
// them blank, then pushes it to the workspace-scoped NATS subject. All
// errors are logged; none are returned, because a missing analytics
// event must never break a user-visible request.
func (p *MemoryPublisher) Publish(ctx context.Context, workspaceID string, event *MemoryEvent) {
	if p == nil || p.nats == nil || event == nil {
		return
	}
	event.EventID, event.EventTime = stampEventMetadata(event.EventID, event.EventTime)

	if err := p.nats.Publish(ctx, workspaceID, event); err != nil {
		p.logger.Warn("failed to publish memory event",
			"workspace_id", workspaceID,
			"drawer_id", event.DrawerID,
			"error", err.Error(),
		)
	}
}

// -----------------------------------------------------------------------------
// UsagePublisher — enqueues UsageEvent River jobs for asynchronous
// insertion into ClickHouse by UsageWriter.
// -----------------------------------------------------------------------------

// UsagePublisher enqueues UsageEvent jobs onto the River queue for
// asynchronous insertion into ClickHouse by UsageWriter. Construction
// is nil-safe: a nil River client makes Publish a no-op.
type UsagePublisher struct {
	river  *pkgriver.Client
	logger *slog.Logger
}

// NewUsagePublisher wires the River client and logger. Either may be
// nil: callers get a working (no-op) publisher back either way.
func NewUsagePublisher(riverClient *pkgriver.Client, logger *slog.Logger) *UsagePublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &UsagePublisher{river: riverClient, logger: logger}
}

// Publish stamps the event with an ID and timestamp if the caller left
// them blank, then inserts a usage_write River job. All errors are
// logged; none are returned, because analytics must never break the
// user's request path.
func (p *UsagePublisher) Publish(ctx context.Context, workspaceID string, event *UsageEvent) {
	if p == nil || p.river == nil || event == nil {
		return
	}
	event.EventID, event.EventTime = stampEventMetadata(event.EventID, event.EventTime)

	if _, err := p.river.Insert(ctx, *event, nil); err != nil {
		p.logger.Warn("failed to enqueue usage event",
			"workspace_id", workspaceID,
			"error", err.Error(),
		)
	}
}
