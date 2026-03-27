package realtime

// Event name constants matching the mobile Socket.IO client contract.
const (
	EventMessageNew     = "message.new"
	EventMessageUpdated = "message.updated"
	EventAgentTyping    = "agent.typing"
	EventAgentStatus    = "agent.status"
)

// MessageEventPayload is the flat payload for message.new and message.updated events.
type MessageEventPayload struct {
	Message any `json:"message"`
}

// AgentTypingPayload is the flat payload for agent.typing events.
type AgentTypingPayload struct {
	ConversationID string `json:"conversation_id"`
	AgentID        string `json:"agent_id"`
	IsTyping       bool   `json:"is_typing"`
}

// AgentStatusPayload is the flat payload for agent.status events.
type AgentStatusPayload struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
}
