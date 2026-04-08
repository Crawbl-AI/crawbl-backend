// Package usagepublisher publishes token usage events to NATS for
// downstream analytics consumers (e.g., ClickHouse writer).
package usagepublisher

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/crawblnats"
)

// UsageEvent is the JSON payload published to NATS.
type UsageEvent struct {
	EventID             string  `json:"event_id"`
	EventTime           string  `json:"event_time"`
	UserID              string  `json:"user_id"`
	WorkspaceID         string  `json:"workspace_id"`
	ConversationID      string  `json:"conversation_id"`
	MessageID           string  `json:"message_id"`
	AgentID             string  `json:"agent_id"`
	AgentDBID           string  `json:"agent_db_id"`
	Model               string  `json:"model"`
	Provider            string  `json:"provider"`
	PromptTokens        int32   `json:"prompt_tokens"`
	CompletionTokens    int32   `json:"completion_tokens"`
	TotalTokens         int32   `json:"total_tokens"`
	ToolUsePromptTokens int32   `json:"tool_use_prompt_tokens"`
	ThoughtsTokens      int32   `json:"thoughts_tokens"`
	CachedTokens        int32   `json:"cached_tokens"`
	CostUSD             float64 `json:"cost_usd"`
	CallSequence        int32   `json:"call_sequence"`
	TurnID              string  `json:"turn_id"`
	SessionID           string  `json:"session_id"`
}

// Publisher publishes usage events to NATS.
type Publisher struct {
	nats   *crawblnats.Client
	logger *slog.Logger
}

// New creates a new usage publisher. If natsClient is nil, publishing is a no-op.
func New(natsClient *crawblnats.Client, logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Publisher{
		nats:   natsClient,
		logger: logger,
	}
}

// Publish sends a usage event to NATS for the given workspace.
func (p *Publisher) Publish(ctx context.Context, workspaceID string, event *UsageEvent) {
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
		p.logger.Warn("failed to publish usage event",
			"workspace_id", workspaceID,
			"error", err.Error(),
		)
	}
}
