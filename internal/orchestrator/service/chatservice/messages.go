package chatservice

import (
	"context"
	"strings"
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
// For swarm conversations, multiple agents may respond (group discussion).
// For per-agent conversations, exactly one agent responds.
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

	// Route to the correct agent for the ZeroClaw webhook.
	responder := resolveResponder(conversation, agents, opts.Mentions)

	// Build slug->agent lookup for resolving turn attribution.
	agentBySlug := make(map[string]*orchestrator.Agent, len(agents))
	for _, agent := range agents {
		agentBySlug[agent.Slug] = agent
	}

	// Signal agents as processing.
	// For swarm: signal all agents (we don't know which will respond).
	// For per-agent: signal only the target agent.
	typingAgents := s.startTyping(ctx, opts.WorkspaceID, conversation, agents, responder)

	// Call ZeroClaw.
	sendOpts := &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   opts.Content.Text,
		SessionID: conversation.ID,
	}
	if responder != nil {
		sendOpts.AgentID = responder.Slug
	}
	turns, mErr := s.runtimeClient.SendText(ctx, sendOpts)

	// Clear typing indicators.
	s.stopTyping(ctx, opts.WorkspaceID, conversation, typingAgents)

	if mErr != nil {
		for _, agent := range typingAgents {
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, agent.ID, string(orchestrator.AgentStatusError))
		}
		return nil, mErr
	}

	// Persist user message + N reply messages in a single transaction.
	return s.persistMessages(ctx, opts, conversation, agents, agentBySlug, turns)
}

// startTyping emits typing indicators and returns the agents that were signaled.
func (s *service) startTyping(ctx context.Context, workspaceID string, conversation *orchestrator.Conversation, agents []*orchestrator.Agent, responder *orchestrator.Agent) []*orchestrator.Agent {
	if conversation.Type == orchestrator.ConversationTypeSwarm {
		for _, agent := range agents {
			s.broadcaster.EmitAgentStatus(ctx, workspaceID, agent.ID, string(orchestrator.AgentStatusBusy))
			s.broadcaster.EmitAgentTyping(ctx, workspaceID, conversation.ID, agent.ID, true)
		}
		return agents
	}
	if responder != nil {
		s.broadcaster.EmitAgentStatus(ctx, workspaceID, responder.ID, string(orchestrator.AgentStatusBusy))
		s.broadcaster.EmitAgentTyping(ctx, workspaceID, conversation.ID, responder.ID, true)
		return []*orchestrator.Agent{responder}
	}
	return nil
}

// stopTyping clears typing indicators for the given agents.
func (s *service) stopTyping(ctx context.Context, workspaceID string, conversation *orchestrator.Conversation, agents []*orchestrator.Agent) {
	for _, agent := range agents {
		s.broadcaster.EmitAgentTyping(ctx, workspaceID, conversation.ID, agent.ID, false)
		s.broadcaster.EmitAgentStatus(ctx, workspaceID, agent.ID, string(orchestrator.AgentStatusOnline))
	}
}

// persistMessages saves the user message and all agent reply messages atomically.
func (s *service) persistMessages(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agents []*orchestrator.Agent,
	agentBySlug map[string]*orchestrator.Agent,
	turns []userswarmclient.AgentTurn,
) ([]*orchestrator.Message, *merrors.Error) {
	now := time.Now().UTC()

	userMsg := &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: conversation.ID,
		Role:           orchestrator.MessageRoleUser,
		Content:        opts.Content,
		Status:         orchestrator.MessageStatusDelivered,
		LocalID:        stringPtr(strings.TrimSpace(opts.LocalID)),
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

	if _, mErr := database.WithTransaction(opts.Sess, "send chat message", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
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

	// Attach agent objects for the response.
	agentByID := mapAgentsByID(agents)
	for _, reply := range replies {
		if reply.AgentID != nil {
			reply.Agent = agentByID[*reply.AgentID]
		}
	}

	// Broadcast all messages via Socket.IO.
	s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, userMsg)
	for _, reply := range replies {
		s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, reply)
	}

	return replies, nil
}

// stringPtr returns a pointer to a trimmed string, or nil if empty.
func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
