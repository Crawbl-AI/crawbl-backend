// Package realtime defines the broadcaster interface and event payload types
// for real-time workspace event delivery.
package realtime

import (
	"context"

	realtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/realtime/v1"
)

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
	EmitMessageChunk(ctx context.Context, workspaceID string, payload *MessageChunkPayload)

	// EmitMessageDone emits a message.done event when streaming is complete.
	EmitMessageDone(ctx context.Context, workspaceID string, payload *MessageDonePayload)

	// EmitAgentTool emits an agent.tool event for tool call activity during streaming.
	EmitAgentTool(ctx context.Context, workspaceID string, payload *AgentToolPayload)

	// EmitMessageStatus emits a message.status event for delivery status transitions.
	EmitMessageStatus(ctx context.Context, workspaceID string, payload *MessageStatusPayload)

	// EmitAgentDelegation emits an agent.delegation event for inter-agent communication.
	EmitAgentDelegation(ctx context.Context, workspaceID string, payload *AgentDelegationPayload)

	// EmitArtifactUpdated emits an artifact.updated event.
	EmitArtifactUpdated(ctx context.Context, workspaceID string, payload *ArtifactEventPayload)

	// EmitWorkflowEvent emits a workflow progress event.
	EmitWorkflowEvent(ctx context.Context, workspaceID string, event string, payload *WorkflowEventPayload)

	// EmitUsageUpdate emits a usage.update event for per-LLM-call token tracking.
	EmitUsageUpdate(ctx context.Context, workspaceID string, payload *UsageUpdatePayload)
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
type AgentStatusPayload = realtimev1.AgentStatusPayload

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
type MessageChunkPayload = realtimev1.MessageChunkPayload

// MessageDonePayload signals stream completion.
type MessageDonePayload = realtimev1.MessageDonePayload

// AgentToolPayload reports tool call activity during streaming.
type AgentToolPayload = realtimev1.AgentToolPayload

// MessageStatusPayload is emitted when a message's delivery status changes.
type MessageStatusPayload = realtimev1.MessageStatusPayload

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
type DelegationAgent = realtimev1.DelegationAgent

// AgentDelegationPayload reports agent-to-agent delegation activity.
type AgentDelegationPayload = realtimev1.AgentDelegationPayload

// ArtifactEventPayload reports artifact creation/update/review activity.
type ArtifactEventPayload = realtimev1.ArtifactEventPayload

// UsageUpdatePayload reports per-LLM-call token usage to connected clients.
type UsageUpdatePayload = realtimev1.UsageUpdatePayload

// WorkflowEventPayload reports workflow execution progress.
type WorkflowEventPayload = realtimev1.WorkflowEventPayload
