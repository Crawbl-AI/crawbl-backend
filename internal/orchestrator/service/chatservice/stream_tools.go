package chatservice

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"

	agentruntimetools "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// handleToolCall processes a StreamEventToolCall: persists a tool_status message,
// emits tool status, and records delegation for transfer_to_agent.
func (ss *streamSession) handleToolCall(ctx context.Context, chunk userswarmclient.StreamChunk) {
	toolAgentID := resolveToolAgentID(ss.primary, ss.lookups, chunk.AgentID)
	parsed := parseToolCallArgs(chunk.Tool, chunk.Args)
	// Per the agent.tool contract the wire carries a human-readable
	// args.description written by the LLM itself (enforced by the
	// agent system prompt) — we pass it through, no server-side
	// switch or hardcoded phrasing. For web tools (web_fetch,
	// http_request) we also forward args.url so mobile can render
	// the target as a tappable link. Every other per-tool field
	// (path, pattern, agent_name, …) is dropped at this boundary
	// because mobile no longer consumes them. Query keeps its
	// top-level slot as a dedup key between the ephemeral event
	// and the persisted message.
	wireArgs := buildWireArgs(chunk.Tool, parsed.Parsed)

	// Persist tool_status message (state: running).
	var toolMsgID string
	var toolCreatedAt string
	if chunk.Tool != agentruntimetools.ToolTransferToAgent {
		toolMsg := ss.newToolStatusMessage(toolAgentID, chunk.Tool, orchestrator.ToolStateRunning, parsed, wireArgs)
		if ss.svc.savePlaceholder(ctx, ss.sess, toolMsg) == nil {
			toolMsgID = toolMsg.ID
			toolCreatedAt = toolMsg.CreatedAt.UTC().Format(time.RFC3339Nano)
			if toolMsg.AgentID != nil {
				toolMsg.Agent = ss.lookups.byID[*toolMsg.AgentID]
			}
			ss.svc.broadcaster.EmitMessageNew(ctx, ss.wsID, toolMsg)
		}
	}

	if chunk.CallID != "" {
		ss.pending[chunk.CallID] = pendingToolCall{tool: chunk.Tool, agentSlug: chunk.AgentID, args: parsed, messageID: toolMsgID}
	}

	ss.svc.broadcaster.EmitAgentTool(ctx, ss.wsID, &realtime.AgentToolPayload{
		AgentId: toolAgentID, ConversationId: ss.convID,
		Tool: chunk.Tool, Status: realtime.AgentToolStatusRunning,
		CallId: chunk.CallID, Query: parsed.Query, Args: toStructPB(wireArgs),
		CreatedAt: toolCreatedAt,
	})
}

// handleToolResult processes a StreamEventToolResult: emits completion and delegation status.
func (ss *streamSession) handleToolResult(ctx context.Context, chunk userswarmclient.StreamChunk) {
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
	doneArgs := buildWireArgs(matched.tool, matched.args.Parsed)

	ss.svc.broadcaster.EmitAgentTool(ctx, ss.wsID, &realtime.AgentToolPayload{
		AgentId: toolAgentID, ConversationId: ss.convID,
		Tool: matched.tool, Status: realtime.AgentToolStatusDone,
		CallId: chunk.CallID, Query: matched.args.Query, Args: toStructPB(doneArgs),
	})

	// Update persisted tool_status message to completed.
	if matched.messageID != "" {
		if mErr := ss.svc.messageRepo.UpdateToolState(ctx, ss.sess, matched.messageID, string(orchestrator.ToolStateCompleted)); mErr != nil {
			slog.Warn("failed to update tool state to completed", "message_id", matched.messageID, "error", mErr)
		}
	}

	if matched.tool == agentruntimetools.ToolTransferToAgent {
		ss.handleTransferToAgent(ctx, matched)
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
func (ss *streamSession) handleTransferToAgent(ctx context.Context, matched pendingToolCall) {
	slug, _ := matched.args.Parsed[agentruntimetools.ToolTransferToAgentArgField].(string)
	del := ss.lookups.bySlug[slug]
	if del == nil {
		return
	}
	ss.svc.broadcaster.EmitAgentStatus(ctx, ss.wsID, del.ID, string(orchestrator.AgentStatusOnline), ss.convID)
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
// and the persisted tool_status message. Contract:
//
//  1. args.description — if the LLM supplied one (enforced by the
//     agent system prompt), forward verbatim after trimming. Missing
//     description means mobile falls back through its own l10n chain
//     (query → localized tool label). We never synthesize an English
//     string here because mobile cannot translate a BE-origin string.
//  2. args.url — for web tools only (web_fetch, http_request), forward
//     the target URL so mobile can render it as a tappable link. For
//     every other tool the URL key is stripped so the field never
//     leaks as dead weight.
//
// A nil return (not an empty map) is what callers should receive when
// there is nothing to send; an allocated empty map would still round-
// trip through structpb and end up as `{}` on the wire for every tool
// call, which is avoidable noise.
func buildWireArgs(tool string, args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := map[string]any{}
	if d, ok := args["description"].(string); ok {
		if trimmed := strings.TrimSpace(d); trimmed != "" {
			out["description"] = trimmed
		}
	}
	if isWebTool(tool) {
		if u, ok := args["url"].(string); ok {
			if trimmed := strings.TrimSpace(u); trimmed != "" {
				out["url"] = trimmed
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isWebTool reports whether the tool is a web/HTTP action whose target
// URL mobile renders as a tappable link. Kept as a tiny allowlist so
// adding a new web tool is a one-line change and no tool can silently
// opt in by having a `url` arg.
func isWebTool(tool string) bool {
	switch tool {
	case agentruntimetools.ToolWebFetch, agentruntimetools.ToolHTTPRequest:
		return true
	}
	return false
}

// toStructPB converts a map[string]any to *structpb.Struct for the proto
// AgentToolPayload.Args field. Returns nil when the input is nil.
func toStructPB(m map[string]any) *structpb.Struct {
	if m == nil {
		return nil
	}
	s, _ := structpb.NewStruct(m)
	return s
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
