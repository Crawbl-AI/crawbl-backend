package chatservice

import (
	"log/slog"
	"strings"
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
	// Per the agent.tool contract the wire carries exactly one
	// human-readable description in args.description. The LLM writes
	// this itself as part of every tool call (enforced by the agent
	// system prompt); we just pass it through — no server-side
	// switch/lookup, no per-tool hardcoded phrasing. The per-tool
	// typed fields (path, url, pattern, agent_name, …) are dropped
	// at this boundary because mobile no longer consumes them.
	// Query keeps its top-level slot because it's still used as a
	// dedup key between the ephemeral event and the persisted message.
	wireArgs := buildWireArgs(parsed.Parsed)

	// Persist tool_status message (state: running).
	var toolMsgID string
	var toolCreatedAt string
	if chunk.Tool != agentruntimetools.ToolTransferToAgent {
		toolMsg := ss.newToolStatusMessage(toolAgentID, chunk.Tool, orchestrator.ToolStateRunning, parsed, wireArgs)
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
		Query: parsed.Query, Args: wireArgs,
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

	// Mirror the description from the paired running event so mobile
	// renders the same sentence on both transitions. If the LLM didn't
	// provide one, the map stays empty and mobile's own fallback chain
	// (query → localized tool label) takes over — we never synthesize
	// an English sentence BE-side because it would dodge mobile l10n.
	doneArgs := buildWireArgs(matched.args.Parsed)

	ss.svc.broadcaster.EmitAgentTool(ss.ctx, ss.wsID, realtime.AgentToolPayload{
		AgentID: toolAgentID, ConversationID: ss.convID,
		Tool: matched.tool, Status: realtime.AgentToolStatusDone,
		Query: matched.args.Query, Args: doneArgs,
	})

	// Update persisted tool_status message to completed.
	if matched.messageID != "" {
		if mErr := ss.svc.messageRepo.UpdateToolState(ss.ctx, ss.sess, matched.messageID, string(orchestrator.ToolStateCompleted)); mErr != nil {
			slog.Warn("failed to update tool state to completed", "message_id", matched.messageID, "error", mErr)
		}
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

// buildWireArgs assembles the args map for the agent.tool socket event
// and the persisted tool_status message. Contract: if the LLM supplied
// a description (enforced by the agent system prompt), forward it
// verbatim; otherwise emit NO description key at all so the mobile
// falls back through its own i18n chain (query → localized tool
// label). We never synthesize an English string here because mobile
// cannot translate a BE-origin string — doing so would hard-code
// English into every non-English client. A missing description is a
// BE/LLM bug to fix upstream, not a gap to paper over at the edge.
func buildWireArgs(args map[string]any) map[string]any {
	out := map[string]any{}
	if args == nil {
		return out
	}
	if d, ok := args["description"].(string); ok {
		if trimmed := strings.TrimSpace(d); trimmed != "" {
			out["description"] = trimmed
		}
	}
	return out
}

// newToolStatusMessage creates a tool_status message for persistence.
// wireArgs is the minimal {description} map the client actually consumes;
// the full parsed args are dropped at this boundary because mobile no
// longer reads the per-tool typed fields.
func (ss *streamSession) newToolStatusMessage(agentID, tool string, state orchestrator.ToolState, parsed toolCallArgs, wireArgs map[string]any) *orchestrator.Message {
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
			Args:  wireArgs,
		},
		Status:      orchestrator.MessageStatusDelivered,
		AgentID:     &agentID,
		Attachments: []orchestrator.Attachment{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
