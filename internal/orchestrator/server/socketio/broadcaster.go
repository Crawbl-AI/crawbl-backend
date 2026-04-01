package socketio

import (
	"context"
	"log/slog"

	"github.com/zishang520/socket.io/v2/socket"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// NewBroadcaster creates a Broadcaster backed by the given Socket.IO server.
func NewBroadcaster(io *socket.Server, logger *slog.Logger) *Broadcaster {
	return &Broadcaster{
		io:     io,
		logger: logger,
	}
}

// Compile-time interface satisfaction check.
var _ realtime.Broadcaster = (*Broadcaster)(nil)

// EmitToWorkspace sends a named event with payload to all clients in a workspace room.
func (b *Broadcaster) EmitToWorkspace(_ context.Context, workspaceID string, event string, data any) {
	room := socket.Room(workspaceRoomPrefix + workspaceID)
	if err := b.io.Of(socketNamespace, nil).To(room).Emit(event, data); err != nil {
		b.logger.Error("socketio broadcast: emit failed",
			"event", event,
			"workspace_id", workspaceID,
			"error", err.Error(),
		)
	}
}

// EmitMessageNew emits a message.new event for a newly created message.
func (b *Broadcaster) EmitMessageNew(ctx context.Context, workspaceID string, data any) {
	b.EmitToWorkspace(ctx, workspaceID, realtime.EventMessageNew, realtime.MessageEventPayload{
		Message: data,
	})
}

// EmitMessageUpdated emits a message.updated event for a modified message.
func (b *Broadcaster) EmitMessageUpdated(ctx context.Context, workspaceID string, data any) {
	b.EmitToWorkspace(ctx, workspaceID, realtime.EventMessageUpdated, realtime.MessageEventPayload{
		Message: data,
	})
}

// EmitAgentTyping emits an agent.typing event indicating whether an agent is typing.
func (b *Broadcaster) EmitAgentTyping(ctx context.Context, workspaceID string, conversationID, agentID string, isTyping bool) {
	b.EmitToWorkspace(ctx, workspaceID, realtime.EventAgentTyping, realtime.AgentTypingPayload{
		ConversationID: conversationID,
		AgentID:        agentID,
		IsTyping:       isTyping,
	})
}

// EmitAgentStatus emits an agent.status event when an agent's availability changes.
func (b *Broadcaster) EmitAgentStatus(ctx context.Context, workspaceID string, agentID string, status string) {
	b.EmitToWorkspace(ctx, workspaceID, realtime.EventAgentStatus, realtime.AgentStatusPayload{
		AgentID: agentID,
		Status:  status,
	})
}
