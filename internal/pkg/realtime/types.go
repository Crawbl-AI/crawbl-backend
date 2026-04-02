package realtime

import "context"

// Broadcaster defines the interface for emitting real-time events to connected clients.
// Implementations may use in-memory rooms, Redis pub/sub, or other transport mechanisms.
// Events are scoped to a workspace — all clients connected to that workspace receive them.
type Broadcaster interface {
	// EmitToWorkspace sends a named event with payload to all clients in a workspace room.
	EmitToWorkspace(ctx context.Context, workspaceID string, event string, data any)

	// EmitMessageNew emits a message.new event for a newly created message.
	EmitMessageNew(ctx context.Context, workspaceID string, data any)

	// EmitMessageUpdated emits a message.updated event for a modified message.
	EmitMessageUpdated(ctx context.Context, workspaceID string, data any)

	// EmitAgentStatus emits an agent.status event with optional conversation context.
	EmitAgentStatus(ctx context.Context, workspaceID string, agentID string, status string, conversationID ...string)
}

// NopBroadcaster is a no-op implementation used when real-time is not configured.
type NopBroadcaster struct{}

// Event name constants matching the mobile Socket.IO client contract.
const (
	EventMessageNew     = "message.new"
	EventMessageUpdated = "message.updated"
	EventAgentStatus    = "agent.status"
)

// MessageEventPayload is the flat payload for message.new and message.updated events.
type MessageEventPayload struct {
	Message any `json:"message"`
}

// AgentStatusPayload is the flat payload for agent.status events.
// ConversationID is set when the status is tied to a specific conversation
// (e.g. "reading", "thinking"). Omitted for workspace-wide statuses like "online".
type AgentStatusPayload struct {
	AgentID        string `json:"agent_id"`
	Status         string `json:"status"`
	ConversationID string `json:"conversation_id,omitempty"`
}
