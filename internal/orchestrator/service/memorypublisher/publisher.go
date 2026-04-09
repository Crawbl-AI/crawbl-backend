// Package memorypublisher publishes raw memory drawer events to NATS for
// downstream consumers (e.g., memory distillation workers).
package memorypublisher

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/crawblnats"
)

// MemoryEvent is the JSON payload published to NATS when a new raw drawer is created.
type MemoryEvent struct {
	EventID     string `json:"event_id"`
	EventTime   string `json:"event_time"`
	WorkspaceID string `json:"workspace_id"`
	DrawerID    string `json:"drawer_id"`
	Wing        string `json:"wing"`
	Room        string `json:"room"`
	MemoryType  string `json:"memory_type"`
	AgentID     string `json:"agent_id"`
	ContentLen  int    `json:"content_len"`
}

// Publisher publishes memory events to NATS.
type Publisher struct {
	nats   *crawblnats.Client
	logger *slog.Logger
}

// New creates a new memory publisher. If natsClient is nil, publishing is a no-op.
func New(natsClient *crawblnats.Client, logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Publisher{
		nats:   natsClient,
		logger: logger,
	}
}

// Publish sends a memory event to NATS.
func (p *Publisher) Publish(ctx context.Context, workspaceID string, event *MemoryEvent) {
	if p == nil || p.nats == nil {
		return
	}
	if event.EventID == "" {
		event.EventID = uuid.NewString()
	}
	if event.EventTime == "" {
		event.EventTime = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := p.nats.Publish(ctx, workspaceID, event); err != nil {
		p.logger.Warn("failed to publish memory event",
			"workspace_id", workspaceID,
			"drawer_id", event.DrawerID,
			"error", err.Error(),
		)
	}
}
