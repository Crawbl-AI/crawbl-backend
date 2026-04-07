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
	if mErr := s.persistUserMessage(ctx, opts, conversation); mErr != nil {
		return nil, mErr
	}

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
	mErr := s.persistUserMessage(ctx, opts, conversation)
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
// agentResult holds the outcome of a parallel agent call in swarm mode.
type agentResult struct {
	replies []*orchestrator.Message
	err     *merrors.Error
}

type pendingToolCall struct {
	tool      string       // e.g. agentruntimetools.ToolTransferToAgent
	agentSlug string       // agent slug from the ToolCallEvent (e.g. "manager")
	args      toolCallArgs // parsed args — reused on ToolResult for delegation resolution
	messageID string       // persisted tool_status message ID for updating on completion
}

// agentSilentResponse is the sentinel text agents return when they have nothing to say.
const agentSilentResponse = "[SILENT]"

// taskPreviewMaxRunes caps the delegation task_preview field.
const taskPreviewMaxRunes = 120

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
// Creates a placeholder, reads chunks from the runtime, emits Socket.IO
// events, and persists the final message(s). Multi-agent: distinct agent_id
// values in chunks get separate placeholder messages.
func (s *service) callAgentStreaming(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	runtimeState *orchestrator.RuntimeStatus,
	agent *orchestrator.Agent,
	lookups agentLookups,
	extraContext string,
) ([]*orchestrator.Message, *merrors.Error) {
	wsID := opts.WorkspaceID
	convID := conversation.ID

	// 1. Emit thinking + create placeholder.
	var log *slog.Logger
	if agent != nil {
		log = slog.With("agent", agent.Slug, "conv", convID)
		s.broadcaster.EmitAgentStatus(ctx, wsID, agent.ID, string(orchestrator.AgentStatusThinking), convID)
	} else {
		log = slog.With("conv", convID)
	}

	placeholder := s.newPlaceholder(conversation.ID, agent)
	if mErr := s.savePlaceholder(ctx, opts.Sess, placeholder); mErr != nil {
		s.broadcaster.EmitAgentStatus(ctx, wsID, agent.ID, string(orchestrator.AgentStatusError))
		return nil, mErr
	}

	// 2. Open gRPC stream.
	streamCh, mErr := s.runtimeClient.SendTextStream(ctx, &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   runtimeMessage(normalizeRuntimeMessage(opts.Content.Text, opts.Mentions), extraContext),
		SessionID: convID,
		AgentID:   runtimeAgentID(agent),
	})
	if mErr != nil {
		log.Warn("stream open failed, removing placeholder", "error", mErr.Error())
		if delErr := s.messageRepo.DeleteByID(ctx, opts.Sess, placeholder.ID); delErr != nil {
			log.Warn("failed to delete placeholder", "error", delErr.Error())
		}
		s.broadcaster.EmitAgentStatus(ctx, wsID, agent.ID, string(orchestrator.AgentStatusError), convID)
		return nil, mErr
	}

	// Mark user message as delivered (once across parallel agents).
	s.emitDeliveredOnce(ctx, opts, convID)

	// 3. Read chunks — route to per-agent streams.
	streamStart := time.Now()
	totalChunks := 0
	globalDone := false
	firstChunk := true

	streams := map[string]*subAgentStream{
		agent.ID: {agent: agent, placeholder: placeholder, firstChunk: true},
	}
	pending := make(map[string]pendingToolCall)

	resolveStream := func(slug string) *subAgentStream {
		if slug == "" || slug == agent.Slug {
			return streams[agent.ID]
		}
		sub := lookups.bySlug[slug]
		if sub == nil {
			return streams[agent.ID]
		}
		if st, ok := streams[sub.ID]; ok {
			return st
		}
		return s.createSubAgentStream(ctx, opts, conversation, agent, sub, streams)
	}

	for chunk := range streamCh {
		switch chunk.Type {
		case userswarmclient.StreamEventChunk, userswarmclient.StreamEventThinking:
			st := resolveStream(chunk.AgentID)
			if firstChunk {
				s.emitReadOnce(ctx, opts, convID)
				firstChunk = false
			}
			if st.firstChunk {
				s.broadcaster.EmitAgentStatus(ctx, wsID, st.agent.ID, string(orchestrator.AgentStatusWriting), convID)
				st.firstChunk = false
			}
			st.accumulated.WriteString(chunk.Delta)
			st.chunkCount++
			totalChunks++
			s.broadcaster.EmitMessageChunk(ctx, wsID, realtime.MessageChunkPayload{
				MessageID: st.placeholder.ID, ConversationID: convID,
				AgentID: st.agent.ID, Chunk: chunk.Delta,
			})

		case userswarmclient.StreamEventToolCall:
			// Resolve stream BEFORE handling the tool call — if this is a
			// sub-agent's tool, createSubAgentStream fires here (emitting
			// agent.delegation running) before the tool_status is persisted.
			resolveStream(chunk.AgentID)
			s.handleToolCall(ctx, opts, conversation, agent, lookups, placeholder, chunk, pending)

		case userswarmclient.StreamEventToolResult:
			s.handleToolResult(ctx, opts, conversation, agent, lookups, placeholder, chunk, pending)

		case userswarmclient.StreamEventDone:
			if chunk.AgentID == "" {
				globalDone = true
				for _, st := range streams {
					st.done = true
				}
			} else {
				resolveStream(chunk.AgentID).done = true
				globalDone = allStreamsDone(streams)
			}
		}
	}

	log.Info("stream complete", "streams", len(streams), "chunks", totalChunks,
		"ms", time.Since(streamStart).Milliseconds())

	// 4. Finalize streams — delegation first, then sub-agents.
	replies := s.finalizeStreams(ctx, opts, conversation, agent, lookups, streams, placeholder, globalDone)

	if len(replies) == 0 {
		return nil, nil
	}
	return replies, nil
}

// newPlaceholder creates a pending agent message placeholder.
func (s *service) newPlaceholder(convID string, agent *orchestrator.Agent) *orchestrator.Message {
	now := time.Now().UTC()
	var agentID *string
	if agent != nil {
		agentID = &agent.ID
	}
	return &orchestrator.Message{
		ID: uuid.NewString(), ConversationID: convID,
		Role:    orchestrator.MessageRoleAgent,
		Content: orchestrator.MessageContent{Type: orchestrator.MessageContentTypeText},
		Status:  orchestrator.MessageStatusPending, AgentID: agentID,
		Attachments: []orchestrator.Attachment{},
		CreatedAt:   now, UpdatedAt: now,
	}
}

// savePlaceholder persists a placeholder message in a transaction.
func (s *service) savePlaceholder(ctx context.Context, sess *dbr.Session, msg *orchestrator.Message) *merrors.Error {
	_, mErr := database.WithTransaction(sess, "create placeholder", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
		if mErr := s.messageRepo.Save(ctx, tx, msg); mErr != nil {
			return nil, mErr
		}
		return msg, nil
	})
	return mErr
}

// emitDeliveredOnce marks the user message as delivered (once across parallel agents).
func (s *service) emitDeliveredOnce(ctx context.Context, opts *orchestratorservice.SendMessageOpts, convID string) {
	if opts.UserMessageID == "" || opts.StatusDeliveredOnce == nil {
		return
	}
	opts.StatusDeliveredOnce.Do(func() {
		s.broadcaster.EmitMessageStatus(ctx, opts.WorkspaceID, realtime.MessageStatusPayload{
			MessageID: opts.UserMessageID, ConversationID: convID,
			LocalID: opts.LocalID, Status: string(orchestrator.MessageStatusDelivered),
		})
		if mErr := s.messageRepo.UpdateStatus(ctx, opts.Sess, opts.UserMessageID, orchestrator.MessageStatusDelivered); mErr != nil {
			slog.Warn("failed to update delivered status", "msg", opts.UserMessageID, "error", mErr.Error())
		}
	})
}

// emitReadOnce marks the user message as read (once on first chunk).
func (s *service) emitReadOnce(ctx context.Context, opts *orchestratorservice.SendMessageOpts, convID string) {
	if opts.UserMessageID == "" || opts.StatusReadOnce == nil {
		return
	}
	opts.StatusReadOnce.Do(func() {
		s.broadcaster.EmitMessageStatus(ctx, opts.WorkspaceID, realtime.MessageStatusPayload{
			MessageID: opts.UserMessageID, ConversationID: convID,
			LocalID: opts.LocalID, Status: string(orchestrator.MessageStatusRead),
		})
	})
}

// createSubAgentStream creates a new placeholder and stream entry for a sub-agent.
func (s *service) createSubAgentStream(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	primary, sub *orchestrator.Agent,
	streams map[string]*subAgentStream,
) *subAgentStream {
	convID := conversation.ID
	placeholder := s.newPlaceholder(convID, sub)
	if mErr := s.savePlaceholder(ctx, opts.Sess, placeholder); mErr != nil {
		slog.Warn("sub-agent placeholder failed, routing to primary", "sub", sub.Slug, "error", mErr.Error())
		return streams[primary.ID]
	}

	s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, sub.ID, string(orchestrator.AgentStatusThinking), convID)

	// Emit delegation running. ADK may not surface transfer_to_agent as a
	// ToolCallEvent, so this is the reliable place to emit delegation —
	// when the first chunk from the sub-agent creates the stream.
	delegationCreatedAt := ""
	if primarySt, ok := streams[primary.ID]; ok {
		delegationCreatedAt = primarySt.placeholder.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	s.broadcaster.EmitAgentDelegation(ctx, opts.WorkspaceID, realtime.AgentDelegationPayload{
		From:           delegationAgent(primary),
		To:             delegationAgent(sub),
		ConversationID: convID, Status: realtime.AgentDelegationStatusRunning,
		MessageID: placeholder.ID,
		CreatedAt: delegationCreatedAt,
	})

	st := &subAgentStream{agent: sub, placeholder: placeholder, firstChunk: true}
	streams[sub.ID] = st
	return st
}

// handleToolCall processes a StreamEventToolCall: persists a tool_status message,
// emits tool status, and records delegation for transfer_to_agent.
func (s *service) handleToolCall(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agent *orchestrator.Agent,
	lookups agentLookups,
	placeholder *orchestrator.Message,
	chunk userswarmclient.StreamChunk,
	pending map[string]pendingToolCall,
) {
	toolAgentID := resolveToolAgentID(agent, lookups, chunk.AgentID)
	parsed := parseToolCallArgs(chunk.Tool, chunk.Args)

	// Persist tool_status message (state: running).
	var toolMsgID string
	var toolCreatedAt string
	if chunk.Tool != agentruntimetools.ToolTransferToAgent {
		toolMsg := s.newToolStatusMessage(conversation.ID, toolAgentID, chunk.Tool, orchestrator.ToolStateRunning, parsed)
		if mErr := s.savePlaceholder(ctx, opts.Sess, toolMsg); mErr == nil {
			toolMsgID = toolMsg.ID
			toolCreatedAt = toolMsg.CreatedAt.UTC().Format(time.RFC3339Nano)
			if toolMsg.AgentID != nil {
				toolMsg.Agent = lookups.byID[*toolMsg.AgentID]
			}
			s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, toolMsg)
		}
	}

	if chunk.CallID != "" {
		pending[chunk.CallID] = pendingToolCall{tool: chunk.Tool, agentSlug: chunk.AgentID, args: parsed, messageID: toolMsgID}
	}

	s.broadcaster.EmitAgentTool(ctx, opts.WorkspaceID, realtime.AgentToolPayload{
		AgentID: toolAgentID, ConversationID: conversation.ID,
		Tool: chunk.Tool, Status: realtime.AgentToolStatusRunning,
		Query: parsed.Query, Args: parsed.Parsed,
		CreatedAt: toolCreatedAt,
	})

	if chunk.Tool == agentruntimetools.ToolTransferToAgent {
		slug, _ := parsed.Parsed[agentruntimetools.ToolTransferToAgentArgField].(string)
		if del := lookups.bySlug[slug]; del != nil {
			go s.recordDelegation(ctx, opts.Sess, opts.WorkspaceID, conversation.ID, placeholder.ID, agent.ID, del.ID, "")
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, del.ID, string(orchestrator.AgentStatusThinking), conversation.ID)

			// Emit delegation running NOW (at transfer_to_agent time) so the mobile
			// shows the delegation card before tool results, not after.
			delegationCreatedAt := placeholder.CreatedAt.UTC().Format(time.RFC3339Nano)
			s.broadcaster.EmitAgentDelegation(ctx, opts.WorkspaceID, realtime.AgentDelegationPayload{
				From:           delegationAgent(agent),
				To:             delegationAgent(del),
				ConversationID: conversation.ID, Status: realtime.AgentDelegationStatusRunning,
				MessageID: placeholder.ID,
				CreatedAt: delegationCreatedAt,
			})
		}
	}
}

// handleToolResult processes a StreamEventToolResult: emits completion and delegation status.
func (s *service) handleToolResult(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agent *orchestrator.Agent,
	lookups agentLookups,
	placeholder *orchestrator.Message,
	chunk userswarmclient.StreamChunk,
	pending map[string]pendingToolCall,
) {
	var matched pendingToolCall
	if chunk.CallID != "" {
		if info, ok := pending[chunk.CallID]; ok {
			matched = info
			delete(pending, chunk.CallID)
		}
	}

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

	s.broadcaster.EmitAgentTool(ctx, opts.WorkspaceID, realtime.AgentToolPayload{
		AgentID: toolAgentID, ConversationID: conversation.ID,
		Tool: matched.tool, Status: realtime.AgentToolStatusDone,
	})

	// Update persisted tool_status message to completed.
	if matched.messageID != "" {
		_ = s.messageRepo.UpdateToolState(ctx, opts.Sess, matched.messageID, string(orchestrator.ToolStateCompleted))
	}

	if matched.tool == agentruntimetools.ToolTransferToAgent {
		slug, _ := matched.args.Parsed[agentruntimetools.ToolTransferToAgentArgField].(string)
		if del := lookups.bySlug[slug]; del != nil {
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, del.ID, string(orchestrator.AgentStatusOnline), conversation.ID)
			go s.completeDelegation("", conversation.ID, placeholder.ID, del.ID)
		}
	}
}

// finalizeStreams persists all stream messages. Delegation (primary) is finalized
// first to guarantee correct timestamp ordering.
func (s *service) finalizeStreams(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agent *orchestrator.Agent,
	lookups agentLookups,
	streams map[string]*subAgentStream,
	placeholder *orchestrator.Message,
	globalDone bool,
) []*orchestrator.Message {
	wsID := opts.WorkspaceID
	convID := conversation.ID
	var replies []*orchestrator.Message

	// Primary agent first when delegation occurred.
	if primarySt := streams[agent.ID]; primarySt != nil && len(streams) > 1 {
		text := strings.TrimSpace(primarySt.accumulated.String())

		// Find the first sub-agent that was delegated to.
		var delegatee *orchestrator.Agent
		for _, st := range streams {
			if st.agent.ID != agent.ID {
				delegatee = st.agent
				break
			}
		}

		primarySt.placeholder.Content = orchestrator.MessageContent{
			Type:        orchestrator.MessageContentTypeDelegation,
			Text:        text,
			From:        orchestrator.ContentAgentFromAgent(agent),
			To:          orchestrator.ContentAgentFromAgent(delegatee),
			Status:      realtime.AgentDelegationStatusCompleted,
			TaskPreview: truncateText(text, taskPreviewMaxRunes),
		}
		if reply := s.finalizeStreamMessage(ctx, opts, conversation, primarySt.placeholder, text, orchestrator.MessageStatusDelegated, lookups); reply != nil {
			replies = append(replies, reply)
		}
		s.updateDelegationSummary(placeholder.ID, text)
		s.broadcaster.EmitAgentStatus(ctx, wsID, primarySt.agent.ID, string(orchestrator.AgentStatusOnline), convID)
	}

	// Remaining streams.
	for _, st := range streams {
		if st.agent.ID == agent.ID && len(streams) > 1 {
			continue
		}
		text := strings.TrimSpace(st.accumulated.String())
		cleanDone := st.done || globalDone

		// Empty — delete placeholder.
		if text == "" {
			if mErr := s.messageRepo.DeleteByID(ctx, opts.Sess, st.placeholder.ID); mErr != nil {
				slog.Warn("delete empty placeholder", "id", st.placeholder.ID, "error", mErr.Error())
			}
			s.broadcaster.EmitAgentStatus(ctx, wsID, st.agent.ID, string(orchestrator.AgentStatusOnline), convID)
			continue
		}

		// Determine status.
		status := orchestrator.MessageStatusDelivered
		switch {
		case text == agentSilentResponse:
			status = orchestrator.MessageStatusSilent
			text = ""
		case !cleanDone:
			status = orchestrator.MessageStatusIncomplete
		}

		if reply := s.finalizeStreamMessage(ctx, opts, conversation, st.placeholder, text, status, lookups); reply != nil {
			replies = append(replies, reply)
		}
		s.broadcaster.EmitMessageDone(ctx, wsID, realtime.MessageDonePayload{
			MessageID: st.placeholder.ID, ConversationID: convID,
			AgentID: st.agent.ID, Status: string(status),
		})
		s.broadcaster.EmitAgentStatus(ctx, wsID, st.agent.ID, string(orchestrator.AgentStatusOnline), convID)
	}

	// Safety sweep: emit delegation completion for all sub-agents.
	for _, st := range streams {
		if st.agent.ID != agent.ID {
			s.broadcaster.EmitAgentDelegation(ctx, wsID, realtime.AgentDelegationPayload{
				From:           delegationAgent(agent),
				To:             delegationAgent(st.agent),
				ConversationID: convID, Status: realtime.AgentDelegationStatusCompleted,
				MessageID: st.placeholder.ID,
			})
		}
	}

	return replies
}

// resolveToolAgentID resolves the agent DB ID for a tool call event.
func resolveToolAgentID(primary *orchestrator.Agent, lookups agentLookups, chunkAgentSlug string) string {
	if chunkAgentSlug != "" {
		if ta := lookups.bySlug[chunkAgentSlug]; ta != nil {
			return ta.ID
		}
	}
	return primary.ID
}

// newToolStatusMessage creates a message with type tool_status for persisting tool calls.
func (s *service) newToolStatusMessage(convID, agentID, tool string, state orchestrator.ToolState, parsed toolCallArgs) *orchestrator.Message {
	now := time.Now().UTC()
	return &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: convID,
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

// allStreamsDone returns true if every stream has received its done event.
func allStreamsDone(streams map[string]*subAgentStream) bool {
	for _, st := range streams {
		if !st.done {
			return false
		}
	}
	return true
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

func (s *service) recordDelegation(_ context.Context, sess *dbr.Session, workspaceID, conversationID, triggerMsgID, delegatorAgentID, delegateAgentID, taskSummary string) {
	auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if mErr := s.messageRepo.RecordDelegation(auditCtx, sess, workspaceID, conversationID, triggerMsgID, delegatorAgentID, delegateAgentID, taskSummary); mErr != nil {
		slog.Warn("recordDelegation: failed to insert delegation row",
			"trigger_message_id", triggerMsgID,
			"delegator", delegatorAgentID,
			"delegate", delegateAgentID,
			"error", mErr.Error(),
		)
	}
}

func (s *service) updateDelegationSummary(triggerMsgID, summary string) {
	if summary == "" {
		return
	}
	auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess := s.db.NewSession(nil)
	if mErr := s.messageRepo.UpdateDelegationSummary(auditCtx, sess, triggerMsgID, summary); mErr != nil {
		slog.Warn("updateDelegationSummary: failed to backfill task_summary",
			"trigger_message_id", triggerMsgID,
			"error", mErr.Error(),
		)
	}
}

// delegationAgent converts an orchestrator.Agent to a realtime.DelegationAgent
// for socket events. Returns nil for nil input.
func delegationAgent(a *orchestrator.Agent) *realtime.DelegationAgent {
	if a == nil {
		return nil
	}
	return &realtime.DelegationAgent{
		ID:     a.ID,
		Name:   a.Name,
		Role:   a.Role,
		Slug:   a.Slug,
		Avatar: a.AvatarURL,
		Status: string(a.Status),
	}
}

// truncateText trims s to maxLen runes with "..." suffix.
func truncateText(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func (s *service) completeDelegation(_, _, triggerMsgID, delegateAgentID string) {
	auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess := s.db.NewSession(nil)
	if mErr := s.messageRepo.CompleteDelegation(auditCtx, sess, triggerMsgID, delegateAgentID); mErr != nil {
		slog.Warn("completeDelegation: failed to mark delegation completed",
			"trigger_message_id", triggerMsgID,
			"delegate", delegateAgentID,
			"error", mErr.Error(),
		)
	}
}

// persistUserMessage saves the user message in its own transaction and
// broadcasts message.new.
func (s *service) persistUserMessage(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
) *merrors.Error {
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
		return mErr
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

	return nil
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
