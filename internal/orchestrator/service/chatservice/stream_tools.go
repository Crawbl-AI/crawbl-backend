package chatservice

import (
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

	toolAgentID := ss.primary.ID
	if matched.agentSlug != "" {
		if ta := ss.lookups.bySlug[matched.agentSlug]; ta != nil {
			toolAgentID = ta.ID
		}
	} else if chunk.AgentID != "" {
		if ta := ss.lookups.bySlug[chunk.AgentID]; ta != nil {
			toolAgentID = ta.ID
		}
	}

	ss.svc.broadcaster.EmitAgentTool(ss.ctx, ss.wsID, realtime.AgentToolPayload{
		AgentID: toolAgentID, ConversationID: ss.convID,
		Tool: matched.tool, Status: realtime.AgentToolStatusDone,
	})

	// Update persisted tool_status message to completed.
	if matched.messageID != "" {
		_ = ss.svc.messageRepo.UpdateToolState(ss.ctx, ss.sess, matched.messageID, string(orchestrator.ToolStateCompleted))
	}

	if matched.tool == agentruntimetools.ToolTransferToAgent {
		slug, _ := matched.args.Parsed[agentruntimetools.ToolTransferToAgentArgField].(string)
		if del := ss.lookups.bySlug[slug]; del != nil {
			ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, del.ID, string(orchestrator.AgentStatusOnline), ss.convID)
			go ss.svc.completeDelegation("", ss.convID, ss.placeholder.ID, del.ID)
		}
	}
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
