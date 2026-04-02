package chatservice

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
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

// sendDirectMessage handles per-agent conversations: one agent, one webhook
// call, atomic persist. Uses typing indicators around the synchronous call.
func (s *service) sendDirectMessage(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agents []*orchestrator.Agent,
	runtimeState *orchestrator.RuntimeStatus,
) ([]*orchestrator.Message, *merrors.Error) {
	responders := resolveResponders(conversation, agents, opts.Mentions)

	var primaryResponder *orchestrator.Agent
	if len(responders) > 0 {
		primaryResponder = responders[0]
	}

	agentBySlug := mapAgentsBySlugs(agents)

	typingAgents := s.startTyping(ctx, opts.WorkspaceID, conversation, agents, primaryResponder)

	sendOpts := &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   opts.Content.Text,
		SessionID: conversation.ID,
	}
	if primaryResponder != nil {
		sendOpts.AgentID = primaryResponder.Slug
		sendOpts.SystemPrompt = agentSystemPrompt(primaryResponder, s.defaultAgents, agents)
	}
	turns, mErr := s.runtimeClient.SendText(ctx, sendOpts)

	s.stopTyping(ctx, opts.WorkspaceID, conversation, typingAgents)

	if mErr != nil {
		for _, agent := range typingAgents {
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusError))
		}
		return nil, mErr
	}

	// Persist user message + agent replies atomically.
	now := time.Now().UTC()

	userMsg := &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: conversation.ID,
		Role:           orchestrator.MessageRoleUser,
		Content:        opts.Content,
		Status:         orchestrator.MessageStatusDelivered,
		LocalID:        stringPtr(opts.LocalID),
		Attachments:    append([]orchestrator.Attachment(nil), opts.Attachments...),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	replies := make([]*orchestrator.Message, 0, len(turns))
	for i, turn := range turns {
		replyAt := now.Add(time.Millisecond * time.Duration(i+1))
		var agentID *string
		if agent := agentBySlug[turn.AgentID]; agent != nil {
			agentID = &agent.ID
		}
		replies = append(replies, &orchestrator.Message{
			ID:             uuid.NewString(),
			ConversationID: conversation.ID,
			Role:           orchestrator.MessageRoleAgent,
			Content: orchestrator.MessageContent{
				Type: orchestrator.MessageContentTypeText,
				Text: turn.Text,
			},
			Status:      orchestrator.MessageStatusDelivered,
			AgentID:     agentID,
			Attachments: []orchestrator.Attachment{},
			CreatedAt:   replyAt,
			UpdatedAt:   replyAt,
		})
	}

	last := replies[len(replies)-1]
	conversation.UpdatedAt = last.CreatedAt
	conversation.LastMessage = last

	if _, mErr := database.WithTransaction(opts.Sess, "send direct message", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
		if mErr := s.messageRepo.Save(ctx, tx, userMsg); mErr != nil {
			return nil, mErr
		}
		for _, reply := range replies {
			if mErr := s.messageRepo.Save(ctx, tx, reply); mErr != nil {
				return nil, mErr
			}
		}
		if mErr := s.conversationRepo.Save(ctx, tx, conversation); mErr != nil {
			return nil, mErr
		}
		return last, nil
	}); mErr != nil {
		return nil, mErr
	}

	agentByID := mapAgentsByID(agents)
	for _, reply := range replies {
		if reply.AgentID != nil {
			reply.Agent = agentByID[*reply.AgentID]
		}
	}

	s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, userMsg)
	for _, reply := range replies {
		s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, reply)
	}

	return replies, nil
}

// sendSwarmMessage handles swarm group chat: persist user message first,
// resolve target agents via mentions or routing, then fire parallel
// goroutines — each agent independently calls ZeroClaw and persists its reply.
func (s *service) sendSwarmMessage(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agents []*orchestrator.Agent,
	runtimeState *orchestrator.RuntimeStatus,
) ([]*orchestrator.Message, *merrors.Error) {
	// 1. Persist user message first so it's visible immediately.
	userMsg, mErr := s.persistUserMessage(ctx, opts, conversation)
	if mErr != nil {
		return nil, mErr
	}

	agentBySlug := mapAgentsBySlugs(agents)
	agentByID := mapAgentsByID(agents)

	// 2. Resolve target agents: mentions first, then routing.
	responders := resolveResponders(conversation, agents, opts.Mentions)

	var targetAgents []*orchestrator.Agent
	if responders != nil {
		targetAgents = responders
	} else {
		// No mentions — ask Manager to route.
		decision, routeErr := s.routeMessage(ctx, runtimeState, conversation.ID, opts.Content.Text, agents)
		if routeErr != nil {
			slog.WarnContext(ctx, "swarm routing failed, falling back to manager",
				"conversation_id", conversation.ID,
				"error", routeErr.Error(),
			)
			// Fallback: route to manager directly.
			decision = &routingDecision{Agents: []string{"manager"}}
		}

		// 3. Check for inline manager response.
		if len(decision.Agents) == 1 && decision.Agents[0] == "manager" && decision.Response != nil {
			managerAgent := agentBySlug["manager"]
			reply, mErr := s.persistAgentMessage(ctx, opts, conversation, managerAgent, *decision.Response, agentByID)
			if mErr != nil {
				return nil, mErr
			}
			return []*orchestrator.Message{reply}, nil
		}

		// 4. Resolve slug strings to agent objects.
		for _, slug := range decision.Agents {
			if agent := agentBySlug[slug]; agent != nil {
				targetAgents = append(targetAgents, agent)
			}
		}

		// Safety net: if all slugs were invalid, fall back to manager.
		if len(targetAgents) == 0 {
			if manager := agentBySlug["manager"]; manager != nil {
				targetAgents = []*orchestrator.Agent{manager}
			} else {
				return nil, merrors.ErrAgentNotFound
			}
		}
	}

	// 5. Fire parallel goroutines per target agent.
	type agentResult struct {
		reply *orchestrator.Message
		err   *merrors.Error
	}
	results := make([]agentResult, len(targetAgents))
	var wg sync.WaitGroup
	wg.Add(len(targetAgents))

	for i, agent := range targetAgents {
		go func(idx int, agent *orchestrator.Agent) {
			defer wg.Done()

			// Emit typing + busy.
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusBusy))
			s.broadcaster.EmitAgentTyping(ctx, opts.WorkspaceID, conversation.ID, agent.ID, true)

			// Call ZeroClaw — per-agent session to prevent cross-contamination
			// when agents respond in parallel. Each agent maintains its own
			// conversation context and doesn't see other agents' current responses.
			turns, callErr := s.runtimeClient.SendText(ctx, &userswarmclient.SendTextOpts{
				Runtime:      runtimeState,
				Message:      opts.Content.Text,
				SessionID:    conversation.ID + ":" + agent.Slug,
				SystemPrompt: agentSystemPrompt(agent, s.defaultAgents, agents),
			})

			// Clear typing.
			s.broadcaster.EmitAgentTyping(ctx, opts.WorkspaceID, conversation.ID, agent.ID, false)

			if callErr != nil {
				s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusError))
				results[idx] = agentResult{err: callErr}
				return
			}

			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusOnline))

			// Collect text from all turns for this agent's response.
			var text string
			if len(turns) > 0 {
				text = strings.TrimSpace(turns[0].Text)
			}
			// Silence is valid — agent chose not to respond. This is normal
			// group chat behavior; not every agent speaks on every message.
			if text == "" || text == "[SILENT]" {
				return
			}

			// Persist this agent's reply independently.
			reply, persistErr := s.persistAgentMessage(ctx, opts, conversation, agent, text, agentByID)
			if persistErr != nil {
				results[idx] = agentResult{err: persistErr}
				return
			}

			results[idx] = agentResult{reply: reply}
		}(i, agent)
	}

	wg.Wait()

	// 6. Collect successful replies.
	var replies []*orchestrator.Message
	var lastErr *merrors.Error
	for _, r := range results {
		if r.reply != nil {
			replies = append(replies, r.reply)
		} else if r.err != nil {
			lastErr = r.err
		}
	}

	if len(replies) == 0 {
		// All agents failed — return the last error encountered.
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, merrors.NewServerErrorText("all agents failed to respond")
	}

	_ = userMsg // user message already persisted and broadcast
	return replies, nil
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
		Status:         orchestrator.MessageStatusDelivered,
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

	var agentID *string
	if agent != nil {
		agentID = &agent.ID
	}

	reply := &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: conversation.ID,
		Role:           orchestrator.MessageRoleAgent,
		Content: orchestrator.MessageContent{
			Type: orchestrator.MessageContentTypeText,
			Text: text,
		},
		Status:      orchestrator.MessageStatusDelivered,
		AgentID:     agentID,
		Attachments: []orchestrator.Attachment{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

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

// startTyping emits typing indicators and returns the agents that were signaled.
// Only used by sendDirectMessage.
func (s *service) startTyping(ctx context.Context, workspaceID string, conversation *orchestrator.Conversation, agents []*orchestrator.Agent, responder *orchestrator.Agent) []*orchestrator.Agent {
	if conversation.Type == orchestrator.ConversationTypeSwarm {
		target := responder
		if target == nil && len(agents) > 0 {
			target = agents[0]
		}
		if target != nil {
			s.broadcaster.EmitAgentStatus(ctx, workspaceID, target.ID, string(orchestrator.AgentStatusBusy))
			s.broadcaster.EmitAgentTyping(ctx, workspaceID, conversation.ID, target.ID, true)
			return []*orchestrator.Agent{target}
		}
		return nil
	}
	if responder != nil {
		s.broadcaster.EmitAgentStatus(ctx, workspaceID, responder.ID, string(orchestrator.AgentStatusBusy))
		s.broadcaster.EmitAgentTyping(ctx, workspaceID, conversation.ID, responder.ID, true)
		return []*orchestrator.Agent{responder}
	}
	return nil
}

// stopTyping clears typing indicators for the given agents.
// Only used by sendDirectMessage.
func (s *service) stopTyping(ctx context.Context, workspaceID string, conversation *orchestrator.Conversation, agents []*orchestrator.Agent) {
	for _, agent := range agents {
		s.broadcaster.EmitAgentTyping(ctx, workspaceID, conversation.ID, agent.ID, false)
		s.broadcaster.EmitAgentStatus(ctx, workspaceID, agent.ID, string(orchestrator.AgentStatusOnline))
	}
}

// stringPtr returns a pointer to a trimmed string, or nil if empty.
func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
