package realtime

// Event name constants matching the mobile Socket.IO client contract.
const (
	EventMessageNew     = "message.new"
	EventMessageUpdated = "message.updated"
	EventAgentTyping    = "agent.typing"
	EventAgentStatus    = "agent.status"
)

// MessageEventPayload is the payload for message.new and message.updated events.
// The "data" field matches the REST API MessageData structure exactly.
type MessageEventPayload struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// AgentTypingPayload is the payload for agent.typing events.
type AgentTypingPayload struct {
	Event string          `json:"event"`
	Data  AgentTypingData `json:"data"`
}

// AgentTypingData carries the per-event fields for agent.typing.
type AgentTypingData struct {
	ConversationID string `json:"conversationId"`
	AgentID        string `json:"agentId"`
	IsTyping       bool   `json:"isTyping"`
}

// AgentStatusPayload is the payload for agent.status events.
type AgentStatusPayload struct {
	Event string          `json:"event"`
	Data  AgentStatusData `json:"data"`
}

// AgentStatusData carries the per-event fields for agent.status.
type AgentStatusData struct {
	AgentID string `json:"agentId"`
	Status  string `json:"status"`
}
