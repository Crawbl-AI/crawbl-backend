package realtime

// Event name constants matching the mobile Socket.IO client contract.
const (
	EventMessageNew     = "message.new"
	EventMessageUpdated = "message.updated"
	EventAgentTyping    = "agent.typing"
	EventAgentStatus    = "agent.status"
)

// MessageEventPayload is the flat payload for message.new and message.updated events.
// Mobile Freezed union expects {message: {...}} at top level.
type MessageEventPayload struct {
	Message any `json:"message"`
}

// AgentTypingPayload is the flat payload for agent.typing events.
// Mobile Freezed union expects {conversationId, agentId, isTyping} at top level.
type AgentTypingPayload struct {
	ConversationID string `json:"conversationId"`
	AgentID        string `json:"agentId"`
	IsTyping       bool   `json:"isTyping"`
}

// AgentStatusPayload is the flat payload for agent.status events.
// Mobile Freezed union expects {agentId, status} at top level.
type AgentStatusPayload struct {
	AgentID string `json:"agentId"`
	Status  string `json:"status"`
}
