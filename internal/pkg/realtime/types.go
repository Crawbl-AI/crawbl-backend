// Package realtime defines the broadcaster interface and event payload types
// for real-time workspace event delivery.
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

	// EmitMessageChunk emits a message.chunk event for a streamed text token.
	EmitMessageChunk(ctx context.Context, workspaceID string, payload MessageChunkPayload)

	// EmitMessageDone emits a message.done event when streaming is complete.
	EmitMessageDone(ctx context.Context, workspaceID string, payload MessageDonePayload)

	// EmitAgentTool emits an agent.tool event for tool call activity during streaming.
	EmitAgentTool(ctx context.Context, workspaceID string, payload AgentToolPayload)

	// EmitMessageStatus emits a message.status event for delivery status transitions.
	EmitMessageStatus(ctx context.Context, workspaceID string, payload MessageStatusPayload)

	// EmitAgentDelegation emits an agent.delegation event for inter-agent communication.
	EmitAgentDelegation(ctx context.Context, workspaceID string, payload AgentDelegationPayload)

	// EmitArtifactUpdated emits an artifact.updated event.
	EmitArtifactUpdated(ctx context.Context, workspaceID string, payload ArtifactEventPayload)

	// EmitWorkflowEvent emits a workflow progress event.
	EmitWorkflowEvent(ctx context.Context, workspaceID string, event string, payload WorkflowEventPayload)

	// EmitUsageUpdate emits a usage.update event for per-LLM-call token tracking.
	EmitUsageUpdate(ctx context.Context, workspaceID string, payload UsageUpdatePayload)
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

// Streaming event names for token-by-token delivery.
const (
	EventMessageChunk  = "message.chunk"
	EventMessageDone   = "message.done"
	EventAgentTool     = "agent.tool"
	EventMessageStatus = "message.status"
)

// EventAgentDelegation is the event name for inter-agent delegation visibility.
const EventAgentDelegation = "agent.delegation"

// EventArtifactUpdated is the event name for artifact updates.
const EventArtifactUpdated = "artifact.updated"

// Event names for workflow progress.
const (
	EventWorkflowStarted       = "workflow.started"
	EventWorkflowStepStarted   = "workflow.step.started"
	EventWorkflowStepCompleted = "workflow.step.completed"
	EventWorkflowStepApproval  = "workflow.step.approval_required"
	EventWorkflowCompleted     = "workflow.completed"
	EventWorkflowFailed        = "workflow.failed"
)

// EventUsageUpdate is the event name for per-LLM-call token usage updates.
const EventUsageUpdate = "usage.update"

// MessageChunkPayload is emitted for each streamed text token.
type MessageChunkPayload struct {
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	AgentID        string `json:"agent_id"`
	Chunk          string `json:"chunk"`
}

// MessageDonePayload signals stream completion.
type MessageDonePayload struct {
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	AgentID        string `json:"agent_id"`
	Status         string `json:"status"` // "delivered" or "failed"
}

// AgentToolPayload reports tool call activity during streaming.
type AgentToolPayload struct {
	AgentID        string         `json:"agent_id"`
	ConversationID string         `json:"conversation_id"`
	Tool           string         `json:"tool"`
	Status         string         `json:"status"` // "running" or "done"
	CallID         string         `json:"call_id,omitempty"`
	Query          string         `json:"query,omitempty"`
	Args           map[string]any `json:"args,omitempty"`
	CreatedAt      string         `json:"created_at,omitempty"`
}

// MessageStatusPayload is emitted when a message's delivery status changes.
type MessageStatusPayload struct {
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	LocalID        string `json:"local_id,omitempty"`
	Status         string `json:"status"` // "sent", "delivered", "read"
}

// AgentDelegationStatus values carried on AgentDelegationPayload.Status
// and persisted to the agent_delegations / agent_messages DB rows.
// These are the single source of truth for the delegation lifecycle —
// every emitter and every DB writer references the constants below so
// the wire value, the DB row, and the mobile client stay in lockstep.
const (
	AgentDelegationStatusRunning   = "running"
	AgentDelegationStatusCompleted = "completed"
	AgentDelegationStatusFailed    = "failed"
)

// AgentToolStatus values carried on AgentToolPayload.Status. Tool
// invocations emit "running" on start and "done" on completion.
const (
	AgentToolStatusRunning = "running"
	AgentToolStatusDone    = "done"
)

// DelegationAgent is the agent summary nested in delegation socket events.
// Matches the JSON shape of orchestrator.ContentAgent and the agent objects
// the mobile already parses in message bubbles.
type DelegationAgent struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Slug   string `json:"slug"`
	Avatar string `json:"avatar"`
	Status string `json:"status"`
}

// AgentDelegationPayload reports agent-to-agent delegation activity.
type AgentDelegationPayload struct {
	From           *DelegationAgent `json:"from"`
	To             *DelegationAgent `json:"to"`
	ConversationID string           `json:"conversation_id"`
	CreatedAt      string           `json:"created_at,omitempty"`
	// Status is one of AgentDelegationStatus* (running | completed | failed).
	Status         string `json:"status"`
	MessagePreview string `json:"message_preview,omitempty"`
	MessageID      string `json:"message_id,omitempty"`
}

// ArtifactEventPayload reports artifact creation/update/review activity.
type ArtifactEventPayload struct {
	ArtifactID     string `json:"artifact_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	Title          string `json:"title"`
	Version        int    `json:"version"`
	Action         string `json:"action"` // "created", "updated", "reviewed"
	AgentID        string `json:"agent_id"`
	AgentSlug      string `json:"agent_slug"`
}

// UsageUpdatePayload reports per-LLM-call token usage to connected clients.
// Emitted inline during streaming so mobile can show real-time token counters.
type UsageUpdatePayload struct {
	AgentID          string `json:"agent_id"`
	ConversationID   string `json:"conversation_id"`
	MessageID        string `json:"message_id"`
	Model            string `json:"model"`
	PromptTokens     int32  `json:"prompt_tokens"`
	CompletionTokens int32  `json:"completion_tokens"`
	TotalTokens      int32  `json:"total_tokens"`
	CallSequence     int32  `json:"call_sequence"`
}

// WorkflowEventPayload reports workflow execution progress.
type WorkflowEventPayload struct {
	WorkflowID     string `json:"workflow_id"`
	ExecutionID    string `json:"execution_id"`
	WorkflowName   string `json:"workflow_name"`
	ConversationID string `json:"conversation_id,omitempty"`
	Status         string `json:"status"` // varies by event type
	StepIndex      int    `json:"step_index,omitempty"`
	StepName       string `json:"step_name,omitempty"`
	AgentSlug      string `json:"agent_slug,omitempty"`
	Error          string `json:"error,omitempty"`
}
