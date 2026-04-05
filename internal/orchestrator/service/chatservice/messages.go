package chatservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	agentruntimetools "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// SendMessage sends a user message and returns the agent replies.
// Dispatches to sendDirectMessage (per-agent conversations) or
// sendSwarmMessage (swarm group chat with parallel agent calls).
func (s *service) SendMessage(ctx context.Context, opts *orchestratorservice.SendMessageOpts) ([]*orchestrator.Message, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}
	if opts.Content.Type != orchestrator.MessageContentTypeText || strings.TrimSpace(opts.Content.Text) == "" {
		return nil, merrors.ErrUnsupportedMessage
	}

	workspace, agents, _, mErr := s.ensureWorkspaceBootstrap(ctx, opts.Sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	conversation, mErr := s.conversationRepo.GetByID(ctx, opts.Sess, opts.WorkspaceID, opts.ConversationID)
	if mErr != nil {
		return nil, mErr
	}

	runtimeState, mErr := s.runtimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          workspace.UserID,
		WorkspaceID:     workspace.ID,
		WaitForVerified: true,
	})
	if mErr != nil {
		return nil, mErr
	}

	for _, agent := range agents {
		agent.Status = orchestrator.StatusForRuntime(runtimeState)
	}

	if conversation.Type == orchestrator.ConversationTypeSwarm {
		return s.sendSwarmMessage(ctx, opts, conversation, agents, runtimeState)
	}
	return s.sendDirectMessage(ctx, opts, conversation, agents, runtimeState)
}

// sendDirectMessage handles per-agent conversations: persist the user message,
// then stream the agent response via callAgentStreaming.
func (s *service) sendDirectMessage(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agents []*orchestrator.Agent,
	runtimeState *orchestrator.RuntimeStatus,
) ([]*orchestrator.Message, *merrors.Error) {
	responders := resolveResponders(conversation, agents, opts.Mentions)
	lookups := newAgentLookups(agents)

	var primaryResponder *orchestrator.Agent
	if len(responders) > 0 {
		primaryResponder = responders[0]
	}

	// Persist user message first (same as swarm path).
	userMsg, mErr := s.persistUserMessage(ctx, opts, conversation)
	if mErr != nil {
		return nil, mErr
	}
	_ = userMsg

	// Stream response from the agent.
	return s.callAgentStreaming(ctx, opts, conversation, runtimeState, primaryResponder, lookups, "")
}

// sendSwarmMessage handles swarm group chat: persist user message first,
// resolve target agents via mentions or Manager, then execute.
func (s *service) sendSwarmMessage(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agents []*orchestrator.Agent,
	runtimeState *orchestrator.RuntimeStatus,
) ([]*orchestrator.Message, *merrors.Error) {
	// 1. Persist user message first so it's visible immediately.
	_, mErr := s.persistUserMessage(ctx, opts, conversation)
	if mErr != nil {
		return nil, mErr
	}

	lookups := newAgentLookups(agents)

	// 2. Resolve target agents: mentions first, then Manager.
	responders := resolveResponders(conversation, agents, opts.Mentions)

	if responders != nil {
		// Mentions resolved — send directly to mentioned agents (parallel).
		return s.executeParallel(ctx, opts, conversation, runtimeState, responders, lookups)
	}

	// 3. No mentions — send to Manager. Manager has full context (memory,
	// conversation history, SOUL.md, tools including delegate) and decides
	// autonomously whether to answer directly or delegate to sub-agents.
	if lookups.manager == nil {
		return nil, merrors.ErrAgentNotFound
	}

	// Build conversation context so Manager sees recent chat history.
	conversationContext := s.buildConversationContext(ctx, opts.Sess, conversation.ID, lookups, 20)

	return s.callAgentStreaming(ctx, opts, conversation, runtimeState, lookups.manager, lookups, conversationContext)
}

// executeParallel fires all agent calls concurrently. Each agent responds
// independently without seeing other agents' current responses.
func (s *service) executeParallel(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	runtimeState *orchestrator.RuntimeStatus,
	targetAgents []*orchestrator.Agent,
	lookups agentLookups,
) ([]*orchestrator.Message, *merrors.Error) {
	type agentResult struct {
		replies []*orchestrator.Message
		err     *merrors.Error
	}
	results := make([]agentResult, len(targetAgents))
	var wg sync.WaitGroup
	wg.Add(len(targetAgents))

	for i, agent := range targetAgents {
		go func(idx int, agent *orchestrator.Agent) {
			defer wg.Done()
			replies, err := s.callAgentStreaming(ctx, opts, conversation, runtimeState, agent, lookups, "")
			results[idx] = agentResult{replies: replies, err: err}
		}(i, agent)
	}

	wg.Wait()

	var replies []*orchestrator.Message
	var lastErr *merrors.Error
	for _, r := range results {
		if len(r.replies) > 0 {
			replies = append(replies, r.replies...)
		}
		if r.err != nil {
			lastErr = r.err
		}
	}

	if len(replies) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return replies, nil
}

// pendingToolCall tracks a ToolCallEvent so we can resolve the tool name,
// agent, and parsed args when the matching ToolResultEvent arrives.
type pendingToolCall struct {
	tool      string       // e.g. agentruntimetools.ToolTransferToAgent
	agentSlug string       // agent slug from the ToolCallEvent (e.g. "manager")
	args      toolCallArgs // parsed args — reused on ToolResult for delegation resolution
}

// subAgentStream tracks a placeholder message and accumulated text for a single
// agent_id seen during a multi-agent streaming response (Phase 5).
type subAgentStream struct {
	agent       *orchestrator.Agent
	placeholder *orchestrator.Message
	accumulated strings.Builder
	chunkCount  int
	firstChunk  bool
	done        bool // received a StreamEventDone for this agent_id
}

// callAgentStreaming handles a single agent's streaming gRPC call.
// Creates a placeholder message, reads ConverseEvents from the runtime pod
// over the gRPC Converse bidi stream, emits Socket.IO events for each
// chunk, and persists the final message as rows arrive — there is no
// batched turns[] flattening step. Each DoneEvent carrying an agent_id
// becomes exactly one messages table row for that agent.
//
// Multi-agent support: the runtime may send chunk/done pairs with
// different agent_id values (e.g. "wally", "eve", "manager"). Each
// distinct agent_id gets its own placeholder message so the mobile
// client shows separate message bubbles per sub-agent.
func (s *service) callAgentStreaming(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	runtimeState *orchestrator.RuntimeStatus,
	agent *orchestrator.Agent,
	lookups agentLookups,
	extraContext string,
) ([]*orchestrator.Message, *merrors.Error) {
	// 1. Emit thinking status.
	s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusThinking), conversation.ID)

	// 2. Create placeholder message in DB (status: pending).
	now := time.Now().UTC()
	var agentID *string
	if agent != nil {
		agentID = &agent.ID
	}
	placeholder := &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: conversation.ID,
		Role:           orchestrator.MessageRoleAgent,
		Content: orchestrator.MessageContent{
			Type: orchestrator.MessageContentTypeText,
			Text: "",
		},
		Status:      orchestrator.MessageStatusPending,
		AgentID:     agentID,
		Attachments: []orchestrator.Attachment{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if _, mErr := database.WithTransaction(opts.Sess, "create placeholder message", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
		if mErr := s.messageRepo.Save(ctx, tx, placeholder); mErr != nil {
			return nil, mErr
		}
		return placeholder, nil
	}); mErr != nil {
		s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusError))
		return nil, mErr
	}

	// 3. Open the Converse bidi stream against the workspace runtime pod.
	streamCh, mErr := s.runtimeClient.SendTextStream(ctx, &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   runtimeMessage(normalizeRuntimeMessage(opts.Content.Text, opts.Mentions), extraContext),
		SessionID: conversation.ID,
		AgentID:   runtimeAgentID(agent),
	})
	if mErr != nil {
		// Stream failed before any data — delete empty placeholder instead of showing empty failed bubble.
		slog.Warn("callAgentStreaming: stream failed, deleting placeholder",
			"agent_slug", agent.Slug,
			"agent_id", agent.ID,
			"error", mErr.Error(),
		)
		_ = s.messageRepo.DeleteByID(ctx, opts.Sess, placeholder.ID)
		s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusError), conversation.ID)
		return nil, mErr
	}

	// User message reached the runtime → mark as delivered (once across parallel agents).
	if opts.UserMessageID != "" && opts.StatusDeliveredOnce != nil {
		opts.StatusDeliveredOnce.Do(func() {
			s.broadcaster.EmitMessageStatus(ctx, opts.WorkspaceID, realtime.MessageStatusPayload{
				MessageID:      opts.UserMessageID,
				ConversationID: conversation.ID,
				LocalID:        opts.LocalID,
				Status:         string(orchestrator.MessageStatusDelivered),
			})
			// Update DB so REST API returns "delivered" for historical messages.
			_ = s.messageRepo.UpdateStatus(ctx, opts.Sess, opts.UserMessageID, orchestrator.MessageStatusDelivered)
		})
	}

	// 4. Read chunks and emit events.
	//
	// Multi-agent streaming (Phase 5): chunks arriving with different agent_id
	// values are routed to separate subAgentStream entries. Each entry holds its
	// own placeholder and accumulator. The primary agent's entry is pre-seeded so
	// single-agent behaviour is unchanged.
	streamStart := time.Now()
	totalChunks := 0
	globalStreamDone := false
	globalFirstChunk := true // guards the "user message read" once-emit

	// streams is keyed by the agent's DB ID (not slug) for stable lookups.
	streams := make(map[string]*subAgentStream)
	streams[agent.ID] = &subAgentStream{
		agent:       agent,
		placeholder: placeholder,
		firstChunk:  true,
	}
	pendingToolCalls := make(map[string]pendingToolCall)

	// resolveStream returns the subAgentStream for the given chunk.AgentID slug,
	// creating a new placeholder and DB row if this is the first chunk for that
	// sub-agent. Falls back to the primary agent stream when the slug is unknown.
	resolveStream := func(chunkAgentSlug string) *subAgentStream {
		// Empty slug or primary agent slug → use primary stream.
		if chunkAgentSlug == "" || chunkAgentSlug == agent.Slug {
			return streams[agent.ID]
		}

		// Look up sub-agent by slug.
		subAgent := lookups.bySlug[chunkAgentSlug]
		if subAgent == nil {
			// Unknown slug — fall back to primary to avoid data loss.
			slog.Warn("callAgentStreaming: unknown sub-agent slug, routing to primary",
				"slug", chunkAgentSlug,
				"primary_agent_id", agent.ID,
			)
			return streams[agent.ID]
		}

		// Already have a stream for this agent.
		if st, exists := streams[subAgent.ID]; exists {
			return st
		}

		// First chunk for this sub-agent — create a new placeholder.
		slog.Info("callAgentStreaming: new sub-agent stream",
			"sub_agent_slug", subAgent.Slug,
			"sub_agent_id", subAgent.ID,
			"conversation_id", conversation.ID,
		)

		subNow := time.Now().UTC()
		subAgentIDPtr := &subAgent.ID
		subPlaceholder := &orchestrator.Message{
			ID:             uuid.NewString(),
			ConversationID: conversation.ID,
			Role:           orchestrator.MessageRoleAgent,
			Content: orchestrator.MessageContent{
				Type: orchestrator.MessageContentTypeText,
				Text: "",
			},
			Status:      orchestrator.MessageStatusPending,
			AgentID:     subAgentIDPtr,
			Attachments: []orchestrator.Attachment{},
			CreatedAt:   subNow,
			UpdatedAt:   subNow,
		}
		if _, mErr := database.WithTransaction(opts.Sess, "create sub-agent placeholder", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
			if mErr := s.messageRepo.Save(ctx, tx, subPlaceholder); mErr != nil {
				return nil, mErr
			}
			return subPlaceholder, nil
		}); mErr != nil {
			slog.Warn("callAgentStreaming: failed to create sub-agent placeholder, routing to primary",
				"sub_agent_slug", subAgent.Slug,
				"sub_agent_id", subAgent.ID,
				"error", mErr.Error(),
			)
			return streams[agent.ID]
		}

		s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, subAgent.ID, string(orchestrator.AgentStatusThinking), conversation.ID)

		// Emit the agent.delegation event so the mobile client can
		// render the delegation card live as the sub-agent picks up
		// the turn. Matches the orchestrator__send_message_to_agent
		// MCP flow — both paths go through EmitAgentDelegation so
		// the client can reuse a single handler regardless of how
		// the delegation was triggered.
		s.broadcaster.EmitAgentDelegation(ctx, opts.WorkspaceID, realtime.AgentDelegationPayload{
			FromAgentID:    agent.ID,
			ToAgentID:      subAgent.ID,
			ConversationID: conversation.ID,
			Status:         realtime.AgentDelegationStatusRunning,
			MessageID:      subPlaceholder.ID,
		})

		st := &subAgentStream{
			agent:       subAgent,
			placeholder: subPlaceholder,
			firstChunk:  true,
		}
		streams[subAgent.ID] = st
		return st
	}

	for chunk := range streamCh {
		switch chunk.Type {
		case userswarmclient.StreamEventChunk, userswarmclient.StreamEventThinking:
			st := resolveStream(chunk.AgentID)

			// Once per stream: mark user message as read.
			if globalFirstChunk {
				if opts.UserMessageID != "" && opts.StatusReadOnce != nil {
					opts.StatusReadOnce.Do(func() {
						s.broadcaster.EmitMessageStatus(ctx, opts.WorkspaceID, realtime.MessageStatusPayload{
							MessageID:      opts.UserMessageID,
							ConversationID: conversation.ID,
							LocalID:        opts.LocalID,
							Status:         string(orchestrator.MessageStatusRead),
						})
					})
				}
				globalFirstChunk = false
			}

			// Once per sub-agent: transition to writing status.
			if st.firstChunk {
				s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, st.agent.ID, string(orchestrator.AgentStatusWriting), conversation.ID)
				st.firstChunk = false
			}

			st.accumulated.WriteString(chunk.Delta)
			st.chunkCount++
			totalChunks++

			s.broadcaster.EmitMessageChunk(ctx, opts.WorkspaceID, realtime.MessageChunkPayload{
				MessageID:      st.placeholder.ID,
				ConversationID: conversation.ID,
				AgentID:        st.agent.ID,
				Chunk:          chunk.Delta,
			})

		// Tool call: agent is about to invoke a tool.
		//
		// 1. Parse the raw JSON args once (structured map + query string).
		// 2. Store the call in pendingToolCalls keyed by call_id so the
		//    matching ToolResult can resolve the tool name and args (the
		//    proto ToolResultEvent only carries call_id + result_json).
		// 3. Emit agent.tool { status: "running" } to mobile.
		// 4. If the tool is transfer_to_agent (ADK delegation), record the
		//    delegation for audit and push the sub-agent into "thinking".
		case userswarmclient.StreamEventToolCall:
			toolAgentID := agent.ID
			if chunk.AgentID != "" {
				if ta := lookups.bySlug[chunk.AgentID]; ta != nil {
					toolAgentID = ta.ID
				}
			}

			// Single parse: JSON → structured args map + human-readable query.
			parsed := parseToolCallArgs(chunk.Tool, chunk.Args)

			// Stash so ToolResult can look up the tool name and original args.
			if chunk.CallID != "" {
				pendingToolCalls[chunk.CallID] = pendingToolCall{tool: chunk.Tool, agentSlug: chunk.AgentID, args: parsed}
			}

			// Mobile receives: tool (enum key for l10n), query (human-readable
			// fallback), args (structured map for custom rendering).
			s.broadcaster.EmitAgentTool(ctx, opts.WorkspaceID, realtime.AgentToolPayload{
				AgentID:        toolAgentID,
				ConversationID: conversation.ID,
				Tool:           chunk.Tool,
				Status:         realtime.AgentToolStatusRunning,
				Query:          parsed.Query,
				Args:           parsed.Parsed,
			})

			// ADK delegation: Manager hands off to a sub-agent.
			if chunk.Tool == agentruntimetools.ToolTransferToAgent {
				slug, _ := parsed.Parsed[agentruntimetools.ToolTransferToAgentArgField].(string)
				if delegateAgent := lookups.bySlug[slug]; delegateAgent != nil {
					go s.recordDelegation(ctx, opts.Sess, opts.WorkspaceID, conversation.ID, placeholder.ID, agent.ID, delegateAgent.ID, "")
					s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, delegateAgent.ID, string(orchestrator.AgentStatusThinking), conversation.ID)
				}
			}

		// ── Tool result: tool invocation completed ───────────────────
		//
		// The proto ToolResultEvent carries only call_id + result_json,
		// NOT the tool name or agent_id. We resolve those from the
		// pendingToolCalls map populated by the preceding ToolCallEvent.
		//
		// 1. Look up the matching pendingToolCall by call_id.
		// 2. Emit agent.tool { status: "done" } with the resolved tool name.
		// 3. If the tool was transfer_to_agent, clear the sub-agent's
		//    "thinking" status back to "online" and record completion.
		case userswarmclient.StreamEventToolResult:
			var matched pendingToolCall
			if chunk.CallID != "" {
				if info, ok := pendingToolCalls[chunk.CallID]; ok {
					matched = info
					delete(pendingToolCalls, chunk.CallID)
				}
			}

			// Resolve the agent that invoked the tool.
			toolAgentID := agent.ID
			if matched.agentSlug != "" {
				if ta := lookups.bySlug[matched.agentSlug]; ta != nil {
					toolAgentID = ta.ID
				}
			} else if chunk.AgentID != "" {
				if ta := lookups.bySlug[chunk.AgentID]; ta != nil {
					toolAgentID = ta.ID
				}
			}

			// Mobile matches this "done" to the earlier "running" by tool name.
			s.broadcaster.EmitAgentTool(ctx, opts.WorkspaceID, realtime.AgentToolPayload{
				AgentID:        toolAgentID,
				ConversationID: conversation.ID,
				Tool:           matched.tool,
				Status:         realtime.AgentToolStatusDone,
			})

			// ADK delegation complete: reset sub-agent status and record audit.
			if matched.tool == agentruntimetools.ToolTransferToAgent {
				slug, _ := matched.args.Parsed[agentruntimetools.ToolTransferToAgentArgField].(string)
				if delegateAgent := lookups.bySlug[slug]; delegateAgent != nil {
					s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, delegateAgent.ID, string(orchestrator.AgentStatusOnline), conversation.ID)
					go s.completeDelegation(opts.WorkspaceID, conversation.ID, placeholder.ID, delegateAgent.ID)
				}
			}

		case userswarmclient.StreamEventDone:
			// Mark done on the specific agent that finished, or globally when
			// agent_id is empty (single-agent legacy behaviour).
			if chunk.AgentID == "" {
				globalStreamDone = true
				// Mark all streams done for finalization.
				for _, st := range streams {
					st.done = true
				}
			} else {
				st := resolveStream(chunk.AgentID)
				st.done = true
				slog.Info("callAgentStreaming: sub-agent done event received",
					"agent_slug", st.agent.Slug,
					"agent_id", st.agent.ID,
					"conversation_id", conversation.ID,
				)
				// If every stream has received its done event, mark global done.
				allDone := true
				for _, st := range streams {
					if !st.done {
						allDone = false
						break
					}
				}
				if allDone {
					globalStreamDone = true
				}
			}
		}
	}

	slog.Info("callAgentStreaming: stream complete",
		"agent_slug", agent.Slug,
		"agent_id", agent.ID,
		"conversation_id", conversation.ID,
		"sub_agent_count", len(streams),
		"total_chunks", totalChunks,
		"elapsed_ms", time.Since(streamStart).Milliseconds(),
	)

	// 5. Finalize each sub-agent stream independently.
	var replies []*orchestrator.Message

	for _, st := range streams {
		finalText := strings.TrimSpace(st.accumulated.String())
		isPrimary := st.agent.ID == agent.ID

		slog.Info("callAgentStreaming: finalizing agent stream",
			"agent_slug", st.agent.Slug,
			"agent_id", st.agent.ID,
			"is_primary", isPrimary,
			"conversation_id", conversation.ID,
			"text_len", len(finalText),
			"chunks", st.chunkCount,
		)

		// If sub-agents answered, suppress Manager's message entirely.
		// Manager's job is to delegate — when delegation happened, only
		// sub-agent bubbles should appear.
		if isPrimary && len(streams) > 1 {
			slog.Info("callAgentStreaming: suppressing Manager bubble (sub-agents answered)",
				"agent_slug", st.agent.Slug,
				"sub_agent_count", len(streams)-1,
			)
			_ = s.messageRepo.DeleteByID(ctx, opts.Sess, st.placeholder.ID)
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, st.agent.ID, string(orchestrator.AgentStatusOnline), conversation.ID)
			continue
		}

		// Determine effective done status for this stream.
		// If the global stream closed properly (all done events received or legacy
		// single-agent done), treat every stream as done.
		streamCompletedCleanly := st.done || globalStreamDone

		// Case A: No output — delete placeholder.
		if finalText == "" && st.chunkCount == 0 {
			if isPrimary {
				slog.Warn("callAgentStreaming: agent produced no output, deleting placeholder",
					"agent_slug", st.agent.Slug,
					"agent_id", st.agent.ID,
					"conversation_id", conversation.ID,
				)
			}
			_ = s.messageRepo.DeleteByID(ctx, opts.Sess, st.placeholder.ID)
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, st.agent.ID, string(orchestrator.AgentStatusOnline), conversation.ID)
			continue
		}

		// Case A2: Primary text was entirely sub-agent lines — nothing left after strip.
		if finalText == "" && st.chunkCount > 0 {
			slog.Info("callAgentStreaming: primary response was all sub-agent lines after strip, deleting placeholder",
				"agent_slug", st.agent.Slug,
				"agent_id", st.agent.ID,
				"conversation_id", conversation.ID,
			)
			_ = s.messageRepo.DeleteByID(ctx, opts.Sess, st.placeholder.ID)
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, st.agent.ID, string(orchestrator.AgentStatusOnline), conversation.ID)
			continue
		}

		// Case B: [SILENT] response.
		if finalText == "[SILENT]" {
			slog.Info("callAgentStreaming: agent responded SILENT",
				"agent_slug", st.agent.Slug,
				"agent_id", st.agent.ID,
				"conversation_id", conversation.ID,
			)
			reply := s.finalizeStreamMessage(ctx, opts, conversation, st.placeholder, "", orchestrator.MessageStatusSilent, lookups)
			s.broadcaster.EmitMessageDone(ctx, opts.WorkspaceID, realtime.MessageDonePayload{
				MessageID:      st.placeholder.ID,
				ConversationID: conversation.ID,
				AgentID:        st.agent.ID,
				Status:         string(orchestrator.MessageStatusSilent),
			})
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, st.agent.ID, string(orchestrator.AgentStatusOnline), conversation.ID)
			if reply != nil {
				replies = append(replies, reply)
			}
			continue
		}

		// Case D: Partial response (stream closed without done event).
		if !streamCompletedCleanly && finalText != "" {
			slog.Warn("callAgentStreaming: incomplete response (stream ended without done event)",
				"agent_slug", st.agent.Slug,
				"agent_id", st.agent.ID,
				"conversation_id", conversation.ID,
				"chunks", st.chunkCount,
				"text_len", len(finalText),
			)
			reply := s.finalizeStreamMessage(ctx, opts, conversation, st.placeholder, finalText, orchestrator.MessageStatusIncomplete, lookups)
			s.broadcaster.EmitMessageDone(ctx, opts.WorkspaceID, realtime.MessageDonePayload{
				MessageID:      st.placeholder.ID,
				ConversationID: conversation.ID,
				AgentID:        st.agent.ID,
				Status:         string(orchestrator.MessageStatusIncomplete),
			})
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, st.agent.ID, string(orchestrator.AgentStatusOnline), conversation.ID)
			if reply != nil {
				replies = append(replies, reply)
			}
			continue
		}

		// Case C: Normal delivered response.
		reply := s.finalizeStreamMessage(ctx, opts, conversation, st.placeholder, finalText, orchestrator.MessageStatusDelivered, lookups)
		s.broadcaster.EmitMessageDone(ctx, opts.WorkspaceID, realtime.MessageDonePayload{
			MessageID:      st.placeholder.ID,
			ConversationID: conversation.ID,
			AgentID:        st.agent.ID,
			Status:         string(orchestrator.MessageStatusDelivered),
		})
		s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, st.agent.ID, string(orchestrator.AgentStatusOnline), conversation.ID)
		if reply != nil {
			replies = append(replies, reply)
		}
	}

	// Safety sweep: clear all sub-agent statuses to online and emit
	// delegation completion for each sub-agent that participated.
	for _, st := range streams {
		if st.agent.ID != agent.ID {
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, st.agent.ID, string(orchestrator.AgentStatusOnline), conversation.ID)
			s.broadcaster.EmitAgentDelegation(ctx, opts.WorkspaceID, realtime.AgentDelegationPayload{
				FromAgentID:    agent.ID,
				ToAgentID:      st.agent.ID,
				ConversationID: conversation.ID,
				Status:         realtime.AgentDelegationStatusCompleted,
				MessageID:      st.placeholder.ID,
			})
		}
	}

	if len(replies) == 0 {
		return nil, nil
	}
	return replies, nil
}

// finalizeStreamMessage updates the placeholder message with final text and status,
// persists it, and broadcasts message.new.
func (s *service) finalizeStreamMessage(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	placeholder *orchestrator.Message,
	text string,
	status orchestrator.MessageStatus,
	lookups agentLookups,
) *orchestrator.Message {
	now := time.Now().UTC()
	placeholder.Content.Text = text
	placeholder.Status = status
	placeholder.UpdatedAt = now

	convCopy := *conversation
	convCopy.UpdatedAt = now
	convCopy.LastMessage = placeholder

	if _, mErr := database.WithTransaction(opts.Sess, "finalize stream message", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
		if mErr := s.messageRepo.Save(ctx, tx, placeholder); mErr != nil {
			return nil, mErr
		}
		if mErr := s.conversationRepo.Save(ctx, tx, &convCopy); mErr != nil {
			return nil, mErr
		}
		return placeholder, nil
	}); mErr != nil {
		// Log but don't fail — the stream already completed.
		return nil
	}

	if placeholder.AgentID != nil {
		placeholder.Agent = lookups.byID[*placeholder.AgentID]
	}

	s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, placeholder)
	return placeholder
}

// buildConversationContext creates a context summary from recent messages
// for injection into agent calls. This gives agents awareness of the
// conversation even though each runtime pod's memory is workspace-isolated.
func (s *service) buildConversationContext(
	ctx context.Context,
	sess *dbr.Session,
	conversationID string,
	lookups agentLookups,
	limit int,
) string {
	if limit == 0 {
		limit = 20
	}

	messages, mErr := s.messageRepo.ListRecent(ctx, sess, conversationID, limit)
	if mErr != nil || len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n## Conversation Context\n")
	sb.WriteString("Recent messages in this conversation (most recent last):\n\n")

	for _, msg := range messages {
		sender := "User"
		if msg.Role == orchestrator.MessageRoleAgent && msg.AgentID != nil {
			if agent := lookups.byID[*msg.AgentID]; agent != nil {
				sender = agent.Name
			}
		}

		text := msg.Content.Text
		if len(text) > 500 {
			text = text[:500] + "..."
		}
		if text == "" || msg.Status == orchestrator.MessageStatusSilent {
			continue
		}

		fmt.Fprintf(&sb, "**%s**: %s\n\n", sender, text)
	}

	return sb.String()
}

// toolCallArgs is the result of parsing a tool call's raw JSON args once.
// Returned by parseToolCallArgs and consumed by the streaming loop to
// populate the agent.tool event (query + args) and delegation tracking.
type toolCallArgs struct {
	// Parsed is the full JSON args as a typed map for the mobile l10n layer.
	Parsed map[string]any
	// Query is the human-readable primary arg extracted via ToolQueryField.
	Query string
}

// parseToolCallArgs unmarshals argsJSON once and extracts both the
// structured args map and the human-readable query string. The query
// field lookup is driven by agentruntimetools.ToolQueryField — no
// switch statements, no magic strings.
func parseToolCallArgs(toolName, argsJSON string) toolCallArgs {
	if argsJSON == "" {
		return toolCallArgs{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &parsed); err != nil {
		return toolCallArgs{}
	}
	if len(parsed) == 0 {
		return toolCallArgs{}
	}

	var query string
	if fields, ok := agentruntimetools.ToolQueryField[toolName]; ok {
		for _, field := range fields {
			if v, ok := parsed[field].(string); ok && v != "" {
				query = v
				break
			}
		}
	}

	return toolCallArgs{Parsed: parsed, Query: query}
}

// recordDelegation inserts an agent_delegations row (async, best-effort).
func (s *service) recordDelegation(ctx context.Context, sess *dbr.Session, workspaceID, conversationID, triggerMsgID, delegatorAgentID, delegateAgentID, taskSummary string) {
	auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = sess.InsertInto("agent_delegations").
		Pair("workspace_id", workspaceID).
		Pair("conversation_id", conversationID).
		Pair("trigger_message_id", triggerMsgID).
		Pair("delegator_agent_id", delegatorAgentID).
		Pair("delegate_agent_id", delegateAgentID).
		Pair("task_summary", taskSummary).
		Pair("status", realtime.AgentDelegationStatusRunning).
		ExecContext(auditCtx)
}

// completeDelegation marks a delegation as completed (async, best-effort).
func (s *service) completeDelegation(workspaceID, conversationID, triggerMsgID, delegateAgentID string) {
	auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess := s.db.NewSession(nil)
	_, _ = sess.Update("agent_delegations").
		Set("status", realtime.AgentDelegationStatusCompleted).
		Set("completed_at", time.Now().UTC()).
		Set("duration_ms", dbr.Expr("EXTRACT(EPOCH FROM (NOW() - created_at))::INTEGER * 1000")).
		Where("trigger_message_id = ? AND delegate_agent_id = ? AND status = ?",
			triggerMsgID, delegateAgentID, realtime.AgentDelegationStatusRunning).
		ExecContext(auditCtx)
}

// persistUserMessage saves the user message in its own transaction and
// broadcasts message.new. Returns the persisted message.
func (s *service) persistUserMessage(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
) (*orchestrator.Message, *merrors.Error) {
	now := time.Now().UTC()

	userMsg := &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: conversation.ID,
		Role:           orchestrator.MessageRoleUser,
		Content:        opts.Content,
		Status:         orchestrator.MessageStatusSent,
		LocalID:        stringPtr(opts.LocalID),
		Attachments:    append([]orchestrator.Attachment(nil), opts.Attachments...),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if _, mErr := database.WithTransaction(opts.Sess, "persist user message", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
		if mErr := s.messageRepo.Save(ctx, tx, userMsg); mErr != nil {
			return nil, mErr
		}
		return userMsg, nil
	}); mErr != nil {
		return nil, mErr
	}

	s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, userMsg)

	// Notify transport layer (e.g. Socket.IO ack) that user message is persisted.
	if opts.OnPersisted != nil {
		opts.OnPersisted(userMsg)
	}

	// Store user message ID for downstream status tracking.
	opts.UserMessageID = userMsg.ID
	opts.StatusDeliveredOnce = &sync.Once{}
	opts.StatusReadOnce = &sync.Once{}

	return userMsg, nil
}

// persistAgentMessage saves one agent reply in its own transaction, updates
// conversation metadata, attaches the agent object, and broadcasts message.new.
// Safe for concurrent calls on the same conversation — last writer wins for
// conversation.UpdatedAt which is acceptable.
func (s *service) persistAgentMessage(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agent *orchestrator.Agent,
	text string,
	agentByID map[string]*orchestrator.Agent,
) (*orchestrator.Message, *merrors.Error) {
	now := time.Now().UTC()
	reply := newAgentMessage(conversation.ID, agent, text, now)

	// Shallow-copy the conversation so concurrent goroutines don't race on
	// UpdatedAt / LastMessage fields of the shared pointer.
	convCopy := *conversation
	convCopy.UpdatedAt = now
	convCopy.LastMessage = reply

	if _, mErr := database.WithTransaction(opts.Sess, "persist agent message", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
		if mErr := s.messageRepo.Save(ctx, tx, reply); mErr != nil {
			return nil, mErr
		}
		if mErr := s.conversationRepo.Save(ctx, tx, &convCopy); mErr != nil {
			return nil, mErr
		}
		return reply, nil
	}); mErr != nil {
		return nil, mErr
	}

	if reply.AgentID != nil {
		reply.Agent = agentByID[*reply.AgentID]
	}

	s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, reply)
	return reply, nil
}

func newAgentMessage(conversationID string, agent *orchestrator.Agent, text string, at time.Time) *orchestrator.Message {
	var agentID *string
	if agent != nil {
		agentID = &agent.ID
	}

	return &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: conversationID,
		Role:           orchestrator.MessageRoleAgent,
		Content: orchestrator.MessageContent{
			Type: orchestrator.MessageContentTypeText,
			Text: text,
		},
		Status:      orchestrator.MessageStatusDelivered,
		AgentID:     agentID,
		Attachments: []orchestrator.Attachment{},
		CreatedAt:   at,
		UpdatedAt:   at,
	}
}

func runtimeMessage(message, extraContext string) string {
	trimmed := strings.TrimSpace(message)
	if extraContext == "" {
		return trimmed
	}
	return trimmed + extraContext
}

// normalizeRuntimeMessage strips structured @mention spans before forwarding the
// message to the runtime pod. The orchestrator has already resolved the target agent,
// so the runtime should receive only the user instruction rather than mobile
// chat routing syntax like "@Wally ...".
func normalizeRuntimeMessage(message string, mentions []orchestrator.Mention) string {
	trimmed := strings.TrimSpace(message)
	if len(mentions) == 0 || trimmed == "" {
		return trimmed
	}

	runes := []rune(message)
	drop := make([]bool, len(runes))

	for _, mention := range mentions {
		if mention.Offset < 0 || mention.Length <= 0 || mention.Offset >= len(runes) {
			continue
		}

		end := mention.Offset + mention.Length
		if end > len(runes) {
			end = len(runes)
		}
		for i := mention.Offset; i < end; i++ {
			drop[i] = true
		}
	}

	var out []rune
	lastWasSpace := false
	for i, r := range runes {
		if drop[i] {
			continue
		}
		if r == '\t' || r == '\n' || r == '\r' {
			r = ' '
		}
		if r == ' ' {
			if lastWasSpace || len(out) == 0 {
				continue
			}
			lastWasSpace = true
			out = append(out, r)
			continue
		}
		lastWasSpace = false
		out = append(out, r)
	}

	normalized := strings.TrimSpace(string(out))
	if normalized == "" {
		return trimmed
	}
	return normalized
}

// The runtime treats an empty agent_id as "use the default manager entrypoint".
// Sub-agents are addressed by slug so the runtime can activate the native
// [agents.<slug>] config for that turn.
func runtimeAgentID(agent *orchestrator.Agent) string {
	if agent == nil || agent.Role == orchestrator.AgentRoleManager {
		return ""
	}
	return agent.Slug
}

// stringPtr returns a pointer to a trimmed string, or nil if empty.
func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
