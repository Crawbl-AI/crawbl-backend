package chatservice

import (
	"log/slog"
	"time"

	"github.com/google/uuid"

	agentruntimetools "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// handleToolCall processes a StreamEventToolCall: persists a tool_status message,
// emits tool status, and records delegation for transfer_to_agent.
func (ss *streamSession) handleToolCall(chunk userswarmclient.StreamChunk) {
	toolAgentID := resolveToolAgentID(ss.primary, ss.lookups, chunk.AgentID)
	parsed := parseToolCallArgs(chunk.Tool, chunk.Args)

	// Persist tool_status message (state: running).
	var toolMsgID string
	var toolCreatedAt string
	if chunk.Tool != agentruntimetools.ToolTransferToAgent {
		toolMsg := ss.newToolStatusMessage(toolAgentID, chunk.Tool, orchestrator.ToolStateRunning, parsed)
		if mErr := ss.svc.savePlaceholder(ss.ctx, ss.sess, toolMsg); mErr == nil {
			toolMsgID = toolMsg.ID
			toolCreatedAt = toolMsg.CreatedAt.UTC().Format(time.RFC3339Nano)
			if toolMsg.AgentID != nil {
				toolMsg.Agent = ss.lookups.byID[*toolMsg.AgentID]
			}
			ss.svc.broadcaster.EmitMessageNew(ss.ctx, ss.wsID, toolMsg)
		}
	}

	if chunk.CallID != "" {
		ss.pending[chunk.CallID] = pendingToolCall{tool: chunk.Tool, agentSlug: chunk.AgentID, args: parsed, messageID: toolMsgID}
	}

	ss.svc.broadcaster.EmitAgentTool(ss.ctx, ss.wsID, realtime.AgentToolPayload{
		AgentID: toolAgentID, ConversationID: ss.convID,
		Tool: chunk.Tool, Status: realtime.AgentToolStatusRunning,
		Query: parsed.Query, Args: parsed.Parsed,
		CreatedAt: toolCreatedAt,
	})
}

// handleToolResult processes a StreamEventToolResult: emits completion and delegation status.
func (ss *streamSession) handleToolResult(chunk userswarmclient.StreamChunk) {
	var matched pendingToolCall
	if chunk.CallID != "" {
		if info, ok := ss.pending[chunk.CallID]; ok {
			matched = info
			delete(ss.pending, chunk.CallID)
		}
	}

	toolAgentID := ss.resolveToolResultAgentID(matched, chunk.AgentID)

	ss.svc.broadcaster.EmitAgentTool(ss.ctx, ss.wsID, realtime.AgentToolPayload{
		AgentID: toolAgentID, ConversationID: ss.convID,
		Tool: matched.tool, Status: realtime.AgentToolStatusDone,
	})

	// Update persisted tool_status message to completed.
	if matched.messageID != "" {
		_ = ss.svc.messageRepo.UpdateToolState(ss.ctx, ss.sess, matched.messageID, string(orchestrator.ToolStateCompleted))
	}

	if matched.tool == agentruntimetools.ToolTransferToAgent {
		ss.handleTransferToAgent(matched)
	}
}

// resolveToolResultAgentID returns the agent DB ID for a tool result event.
// Prefers the calling agent's slug from the matched pending call, then the chunk's agent slug.
func (ss *streamSession) resolveToolResultAgentID(matched pendingToolCall, chunkAgentID string) string {
	if matched.agentSlug != "" {
		if ta := ss.lookups.bySlug[matched.agentSlug]; ta != nil {
			return ta.ID
		}
	}
	if chunkAgentID != "" {
		if ta := ss.lookups.bySlug[chunkAgentID]; ta != nil {
			return ta.ID
		}
	}
	return ss.primary.ID
}

// handleTransferToAgent fires the delegation goroutine when a transfer_to_agent tool completes.
func (ss *streamSession) handleTransferToAgent(matched pendingToolCall) {
	slug, _ := matched.args.Parsed[agentruntimetools.ToolTransferToAgentArgField].(string)
	del := ss.lookups.bySlug[slug]
	if del == nil {
		return
	}
	ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, del.ID, string(orchestrator.AgentStatusOnline), ss.convID)
	triggerMsgID := ss.placeholder.ID
	delegateAgentID := del.ID
	userID := ss.userID
	convID := ss.convID
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("completeDelegation goroutine panic",
					"panic", r,
					"user_id", userID,
					"conv_id", convID,
					"trigger_message_id", triggerMsgID,
					"delegate_agent_id", delegateAgentID,
				)
			}
		}()
		ss.svc.completeDelegation("", convID, triggerMsgID, delegateAgentID)
	}()
}

// newToolStatusMessage creates a tool_status message for persistence.
func (ss *streamSession) newToolStatusMessage(agentID, tool string, state orchestrator.ToolState, parsed toolCallArgs) *orchestrator.Message {
	now := time.Now().UTC()
	return &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: ss.convID,
		Role:           orchestrator.MessageRoleAgent,
		Content: orchestrator.MessageContent{
			Type:  orchestrator.MessageContentTypeToolStatus,
			Tool:  tool,
			State: state,
			Query: parsed.Query,
			Args:  parsed.Parsed,
		},
		Status:      orchestrator.MessageStatusDelivered,
		AgentID:     &agentID,
		Attachments: []orchestrator.Attachment{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
