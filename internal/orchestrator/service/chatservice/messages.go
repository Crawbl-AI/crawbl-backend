package chatservice

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

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
		agent.Status = statusForRuntime(runtimeState)
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
// resolve target agents via mentions or Routing LLM, then execute.
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

	// 2. Resolve target agents: mentions first, then Routing LLM.
	responders := resolveResponders(conversation, agents, opts.Mentions)

	if responders != nil {
		// Mentions resolved — skip Routing LLM entirely.
		return s.executeParallel(ctx, opts, conversation, runtimeState, responders, lookups)
	}

	// 3. No mentions — use Routing LLM (invisible infrastructure switch).
	if lookups.manager != nil {
		s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, lookups.manager.ID, string(orchestrator.AgentStatusReading), conversation.ID)
	}

	decision := s.router.Route(ctx, opts.Content.Text, agents)

	// Clear Manager's reading indicator.
	if lookups.manager != nil {
		s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, lookups.manager.ID, string(orchestrator.AgentStatusOnline))
	}

	// 4. Branch on routing decision.
	if decision.Type == routingTypeSimple {
		// Simple — Manager answers with full ZeroClaw context.
		if lookups.manager == nil {
			return nil, merrors.ErrAgentNotFound
		}
		return s.callAgentAndPersist(ctx, opts, conversation, runtimeState, lookups.manager, lookups)
	}

	// Group — resolve task slugs to agent objects, execute in parallel.
	var targetAgents []*orchestrator.Agent
	agentTasks := make(map[string]string) // slug -> task

	for _, task := range decision.Tasks {
		if agent := lookups.bySlug[task.Slug]; agent != nil {
			targetAgents = append(targetAgents, agent)
			agentTasks[task.Slug] = task.Task
		}
	}

	// Safety net: if all slugs were invalid, fall back to Manager.
	if len(targetAgents) == 0 {
		if lookups.manager != nil {
			return s.callAgentAndPersist(ctx, opts, conversation, runtimeState, lookups.manager, lookups)
		}
		return nil, merrors.ErrAgentNotFound
	}

	// Execute group — parallel goroutines, each agent gets its specific task.
	return s.executeGroup(ctx, opts, conversation, runtimeState, targetAgents, agentTasks, lookups)
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

// callAgentAndPersist is a convenience wrapper for the simple (Manager) path.
// Calls a single agent via streaming with no extra context and returns the persisted replies.
func (s *service) callAgentAndPersist(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	runtimeState *orchestrator.RuntimeStatus,
	agent *orchestrator.Agent,
	lookups agentLookups,
) ([]*orchestrator.Message, *merrors.Error) {
	return s.callAgentStreaming(ctx, opts, conversation, runtimeState, agent, lookups, "")
}

// executeGroup fires parallel goroutines where each agent gets a SPECIFIC task
// from the Routing LLM, not the raw user message.
func (s *service) executeGroup(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	runtimeState *orchestrator.RuntimeStatus,
	targetAgents []*orchestrator.Agent,
	agentTasks map[string]string,
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
			// Use the specific task from routing, not the raw user message.
			task := agentTasks[agent.Slug]
			if task == "" {
				task = opts.Content.Text // fallback to raw message
			}
			replies, err := s.callAgentStreaming(ctx, opts, conversation, runtimeState, agent, lookups, "\n\nYour task: "+task)
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

// callAgentStreaming handles a single agent's streaming webhook call.
// Creates a placeholder message, reads streaming chunks from ZeroClaw,
// emits Socket.IO events for each chunk, and persists the final message.
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

	// 3. Start streaming from ZeroClaw.
	streamCh, mErr := s.runtimeClient.SendTextStream(ctx, &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   runtimeMessage(normalizeRuntimeMessage(opts.Content.Text, opts.Mentions), extraContext),
		SessionID: conversation.ID,
		AgentID:   runtimeAgentID(agent),
	})
	if mErr != nil {
		// Update placeholder to failed.
		s.finalizeStreamMessage(ctx, opts, conversation, placeholder, "", orchestrator.MessageStatusFailed, lookups)
		s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusError))
		return nil, mErr
	}

	// User message reached ZeroClaw → mark as delivered (once across parallel agents).
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
	var accumulated strings.Builder
	firstChunk := true

	for chunk := range streamCh {
		switch chunk.Type {
		case userswarmclient.StreamEventChunk, userswarmclient.StreamEventThinking:
			if firstChunk {
				s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusWriting), conversation.ID)
				// First token received → mark user message as read (once across parallel agents).
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
				firstChunk = false
			}
			accumulated.WriteString(chunk.Delta)
			s.broadcaster.EmitMessageChunk(ctx, opts.WorkspaceID, realtime.MessageChunkPayload{
				MessageID:      placeholder.ID,
				ConversationID: conversation.ID,
				AgentID:        agent.ID,
				Chunk:          chunk.Delta,
			})

		case userswarmclient.StreamEventToolCall:
			s.broadcaster.EmitAgentTool(ctx, opts.WorkspaceID, realtime.AgentToolPayload{
				AgentID:        agent.ID,
				ConversationID: conversation.ID,
				Tool:           chunk.Tool,
				Status:         "running",
				Query:          chunk.Args,
			})

		case userswarmclient.StreamEventToolResult:
			s.broadcaster.EmitAgentTool(ctx, opts.WorkspaceID, realtime.AgentToolPayload{
				AgentID:        agent.ID,
				ConversationID: conversation.ID,
				Tool:           chunk.Tool,
				Status:         "done",
			})

		case userswarmclient.StreamEventDone:
			// Done is handled after the loop.
		}
	}

	// 5. Finalize: update message in DB, emit done, set online.
	finalText := strings.TrimSpace(accumulated.String())
	if finalText == "" || finalText == "[SILENT]" {
		// Agent had nothing to say — remove placeholder.
		// For now, mark as delivered with empty text (Flutter filters [SILENT]).
		finalText = ""
	}

	reply := s.finalizeStreamMessage(ctx, opts, conversation, placeholder, finalText, orchestrator.MessageStatusDelivered, lookups)

	s.broadcaster.EmitMessageDone(ctx, opts.WorkspaceID, realtime.MessageDonePayload{
		MessageID:      placeholder.ID,
		ConversationID: conversation.ID,
		AgentID:        agent.ID,
		Status:         string(orchestrator.MessageStatusDelivered),
	})
	s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusOnline))

	if reply == nil {
		return nil, nil
	}
	return []*orchestrator.Message{reply}, nil
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
// message to ZeroClaw. The orchestrator has already resolved the target agent,
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

// ZeroClaw treats an empty agent_id as "use the default manager entrypoint".
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
