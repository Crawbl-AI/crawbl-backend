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
	lookups := newAgentLookups(agents)

	var primaryResponder *orchestrator.Agent
	if len(responders) > 0 {
		primaryResponder = responders[0]
	}

	typingAgents := s.startTyping(ctx, opts.WorkspaceID, conversation, agents, primaryResponder)

	sendOpts := &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   opts.Content.Text,
		SessionID: conversation.ID,
	}
	sendOpts.AgentID = runtimeAgentID(primaryResponder)
	turns, mErr := s.runtimeClient.SendText(ctx, sendOpts)

	s.stopTyping(ctx, opts.WorkspaceID, conversation, typingAgents)

	if mErr != nil {
		for _, agent := range typingAgents {
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusError))
		}
		return nil, mErr
	}

	replySpecs := buildReplySpecs(turns, lookups.bySlug, primaryResponder)
	if len(replySpecs) == 0 {
		return nil, merrors.NewServerErrorText("runtime returned no visible turns")
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

	replies := buildAgentMessages(conversation.ID, replySpecs, now)

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

	attachReplyAgents(replies, lookups.byID)

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
	_, mErr := s.persistUserMessage(ctx, opts, conversation)
	if mErr != nil {
		return nil, mErr
	}

	lookups := newAgentLookups(agents)

	// 2. Resolve target agents: mentions first, then routing.
	responders := resolveResponders(conversation, agents, opts.Mentions)

	var targetAgents []*orchestrator.Agent
	routingMode := routingModeParallel // default for mentions and fallback

	if responders != nil {
		targetAgents = responders
	} else {
		// No mentions — ask the manager-role agent to route.
		// Show it as reading during routing so the mobile has feedback.
		managerAgent := lookups.manager
		if managerAgent != nil {
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, managerAgent.ID, string(orchestrator.AgentStatusReading), conversation.ID)
		}

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
		if len(decision.Agents) == 1 && decision.Agents[0] == "manager" && decision.Response != nil && managerAgent != nil {
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, managerAgent.ID, string(orchestrator.AgentStatusOnline))
			reply, mErr := s.persistAgentMessage(ctx, opts, conversation, managerAgent, *decision.Response, lookups.byID)
			if mErr != nil {
				return nil, mErr
			}
			return []*orchestrator.Message{reply}, nil
		}

		// 4. Extract execution mode from routing decision.
		if decision.Mode == routingModeSequential {
			routingMode = routingModeSequential
		}

		// 5. Resolve slug strings to agent objects.
		for _, slug := range decision.Agents {
			if agent := lookups.bySlug[slug]; agent != nil {
				targetAgents = append(targetAgents, agent)
			}
		}

		// Safety net: if all slugs were invalid, fall back to manager.
		if len(targetAgents) == 0 {
			if managerAgent != nil {
				targetAgents = []*orchestrator.Agent{managerAgent}
			} else {
				return nil, merrors.ErrAgentNotFound
			}
		}
		// Clear Manager's routing typing indicator now that routing is done.
		if managerAgent != nil {
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, managerAgent.ID, string(orchestrator.AgentStatusOnline))
		}
	}

	// 5. Execute agent calls based on mode.
	if routingMode == routingModeSequential {
		return s.executeSequential(ctx, opts, conversation, runtimeState, targetAgents, lookups)
	}
	return s.executeParallel(ctx, opts, conversation, runtimeState, targetAgents, lookups)
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
			replies, err := s.callAgent(ctx, opts, conversation, runtimeState, agent, lookups, "")
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

// executeSequential calls agents one at a time. Each agent sees the prior
// agents' responses as context, creating a natural discussion flow where
// agents react to each other — like people in a WhatsApp group.
func (s *service) executeSequential(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	runtimeState *orchestrator.RuntimeStatus,
	targetAgents []*orchestrator.Agent,
	lookups agentLookups,
) ([]*orchestrator.Message, *merrors.Error) {
	var replies []*orchestrator.Message
	var priorResponses []string
	var lastErr *merrors.Error

	const maxPriorContext = 3 // keep only the last N responses to avoid bloating the context window

	for _, agent := range targetAgents {
		// Build context from recent prior responses so this agent can react.
		// Cap to the last maxPriorContext entries to avoid eating the context window.
		var discussionContext string
		if len(priorResponses) > 0 {
			recent := priorResponses
			if len(recent) > maxPriorContext {
				recent = recent[len(recent)-maxPriorContext:]
			}
			discussionContext = "\n\nOther agents have already responded:\n" + strings.Join(recent, "\n") +
				"\n\nReact to their responses if relevant, or add your own perspective. Say [SILENT] if nothing to add."
		}

		agentReplies, err := s.callAgent(ctx, opts, conversation, runtimeState, agent, lookups, discussionContext)
		if err != nil {
			lastErr = err
			continue
		}
		for _, reply := range agentReplies {
			replies = append(replies, reply)
			speaker := agent.Name
			if reply.Agent != nil {
				speaker = reply.Agent.Name
			}
			priorResponses = append(priorResponses, "- "+speaker+": "+reply.Content.Text)
		}
	}

	if len(replies) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return replies, nil
}

// callAgent handles a single agent's webhook call: emits thinking status,
// calls ZeroClaw, handles silence, persists and broadcasts the response.
func (s *service) callAgent(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	runtimeState *orchestrator.RuntimeStatus,
	agent *orchestrator.Agent,
	lookups agentLookups,
	extraContext string,
) ([]*orchestrator.Message, *merrors.Error) {
	// Emit thinking status.
	s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusThinking), conversation.ID)

	turns, callErr := s.runtimeClient.SendText(ctx, &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   runtimeMessage(opts.Content.Text, extraContext),
		SessionID: conversation.ID,
		AgentID:   runtimeAgentID(agent),
	})

	if callErr != nil {
		s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusError))
		return nil, callErr
	}

	s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusOnline))

	return s.persistAgentTurns(ctx, opts, conversation, turns, agent, lookups)
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

type replySpec struct {
	agent *orchestrator.Agent
	text  string
}

func buildAgentMessages(conversationID string, replySpecs []replySpec, start time.Time) []*orchestrator.Message {
	replies := make([]*orchestrator.Message, 0, len(replySpecs))
	for i, spec := range replySpecs {
		replyAt := start.Add(time.Millisecond * time.Duration(i+1))
		replies = append(replies, newAgentMessage(conversationID, spec.agent, spec.text, replyAt))
	}
	return replies
}

func buildReplySpecs(turns []userswarmclient.AgentTurn, agentBySlug map[string]*orchestrator.Agent, fallbackAgent *orchestrator.Agent) []replySpec {
	specs := make([]replySpec, 0, len(turns))
	for _, turn := range turns {
		text := strings.TrimSpace(turn.Text)
		if text == "" || text == "[SILENT]" {
			continue
		}

		agent := fallbackAgent
		if turn.AgentID != "" {
			if resolved := agentBySlug[turn.AgentID]; resolved != nil {
				agent = resolved
			}
		}

		specs = append(specs, replySpec{
			agent: agent,
			text:  text,
		})
	}
	return specs
}

func attachReplyAgents(replies []*orchestrator.Message, agentByID map[string]*orchestrator.Agent) {
	for _, reply := range replies {
		if reply.AgentID != nil {
			reply.Agent = agentByID[*reply.AgentID]
		}
	}
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

func (s *service) persistAgentTurns(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	turns []userswarmclient.AgentTurn,
	fallbackAgent *orchestrator.Agent,
	lookups agentLookups,
) ([]*orchestrator.Message, *merrors.Error) {
	replySpecs := buildReplySpecs(turns, lookups.bySlug, fallbackAgent)
	if len(replySpecs) == 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	replies := buildAgentMessages(conversation.ID, replySpecs, now)

	convCopy := *conversation
	convCopy.UpdatedAt = replies[len(replies)-1].CreatedAt
	convCopy.LastMessage = replies[len(replies)-1]

	if _, mErr := database.WithTransaction(opts.Sess, "persist agent turns", func(tx *dbr.Tx) ([]*orchestrator.Message, *merrors.Error) {
		for _, reply := range replies {
			if mErr := s.messageRepo.Save(ctx, tx, reply); mErr != nil {
				return nil, mErr
			}
		}
		if mErr := s.conversationRepo.Save(ctx, tx, &convCopy); mErr != nil {
			return nil, mErr
		}
		return replies, nil
	}); mErr != nil {
		return nil, mErr
	}

	attachReplyAgents(replies, lookups.byID)
	for _, reply := range replies {
		s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, reply)
	}

	return replies, nil
}

func runtimeMessage(message, extraContext string) string {
	trimmed := strings.TrimSpace(message)
	if extraContext == "" {
		return trimmed
	}
	return trimmed + extraContext
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

// startTyping emits typing indicators and returns the agents that were signaled.
// Only used by sendDirectMessage.
func (s *service) startTyping(ctx context.Context, workspaceID string, conversation *orchestrator.Conversation, agents []*orchestrator.Agent, responder *orchestrator.Agent) []*orchestrator.Agent {
	if conversation.Type == orchestrator.ConversationTypeSwarm {
		target := responder
		if target == nil && len(agents) > 0 {
			target = agents[0]
		}
		if target != nil {
			s.broadcaster.EmitAgentStatus(ctx, workspaceID, target.ID, string(orchestrator.AgentStatusThinking), conversation.ID)
			return []*orchestrator.Agent{target}
		}
		return nil
	}
	if responder != nil {
		s.broadcaster.EmitAgentStatus(ctx, workspaceID, responder.ID, string(orchestrator.AgentStatusThinking), conversation.ID)
		return []*orchestrator.Agent{responder}
	}
	return nil
}

// stopTyping clears typing indicators for the given agents.
// Only used by sendDirectMessage.
func (s *service) stopTyping(ctx context.Context, workspaceID string, _ *orchestrator.Conversation, agents []*orchestrator.Agent) {
	for _, agent := range agents {
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
