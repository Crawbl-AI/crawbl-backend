package chatservice

import (
	"context"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// ListConversations retrieves all conversations for a workspace, enriched
// with agent info and the latest message.
func (s *service) ListConversations(ctx context.Context, opts *orchestratorservice.ListConversationsOpts) ([]*orchestrator.Conversation, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	workspace, agents, conversations, mErr := s.ensureWorkspaceBootstrap(ctx, opts.Sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	s.enrichAgentStatus(ctx, workspace, agents)

	agentByID := mapAgentsByID(agents)

	// Batch-fetch the latest message for all conversations in one query
	// instead of one GetLatestByConversationID call per conversation.
	ids := make([]string, len(conversations))
	for i, c := range conversations {
		ids[i] = c.ID
	}
	latestByConvID, mErr := s.messageRepo.GetLatestByConversationIDs(ctx, opts.Sess, ids)
	if mErr != nil {
		return nil, mErr
	}

	for _, conversation := range conversations {
		if conversation.AgentID != nil {
			conversation.Agent = agentByID[*conversation.AgentID]
		}
		if latest, ok := latestByConvID[conversation.ID]; ok {
			if latest.AgentID != nil {
				latest.Agent = agentByID[*latest.AgentID]
			}
			conversation.LastMessage = latest
		}
	}

	return conversations, nil
}

// GetConversation retrieves a single conversation enriched with agent and message data.
func (s *service) GetConversation(ctx context.Context, opts *orchestratorservice.GetConversationOpts) (*orchestrator.Conversation, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	workspace, agents, _, mErr := s.ensureWorkspaceBootstrap(ctx, opts.Sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	s.enrichAgentStatus(ctx, workspace, agents)

	conversation, mErr := s.conversationRepo.GetByID(ctx, opts.Sess, opts.WorkspaceID, opts.ConversationID)
	if mErr != nil {
		return nil, mErr
	}

	s.attachConversationData(ctx, opts.Sess, conversation, mapAgentsByID(agents))
	return conversation, nil
}

// ListMessages retrieves paginated messages for a conversation.
func (s *service) ListMessages(ctx context.Context, opts *orchestratorservice.ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	workspace, agents, _, mErr := s.ensureWorkspaceBootstrap(ctx, opts.Sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	s.enrichAgentStatus(ctx, workspace, agents)

	if _, mErr := s.conversationRepo.GetByID(ctx, opts.Sess, opts.WorkspaceID, opts.ConversationID); mErr != nil {
		return nil, mErr
	}

	page, mErr := s.messageRepo.ListByConversationID(ctx, opts.Sess, &orchestratorrepo.ListMessagesOpts{
		ConversationID: opts.ConversationID,
		ScrollID:       opts.ScrollID,
		Limit:          opts.Limit,
	})
	if mErr != nil {
		return nil, mErr
	}

	agentByID := mapAgentsByID(agents)
	for _, message := range page.Data {
		if message.AgentID != nil {
			message.Agent = agentByID[*message.AgentID]
		}
	}

	return page, nil
}

// CreateConversation creates a new conversation within a workspace.
// For agent-type conversations, the specified agent must exist in the workspace.
func (s *service) CreateConversation(ctx context.Context, opts *orchestratorservice.CreateConversationOpts) (*orchestrator.Conversation, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}
	if strings.TrimSpace(opts.UserID) == "" || strings.TrimSpace(opts.WorkspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}
	if opts.Type != orchestrator.ConversationTypeSwarm && opts.Type != orchestrator.ConversationTypeAgent {
		return nil, merrors.ErrInvalidInput
	}

	// Verify workspace ownership.
	if _, mErr := s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, opts.WorkspaceID); mErr != nil {
		return nil, mErr
	}

	var agentID *string
	if opts.Type == orchestrator.ConversationTypeAgent {
		if strings.TrimSpace(opts.AgentID) == "" {
			return nil, merrors.ErrInvalidInput
		}
		agent, mErr := s.agentRepo.GetByIDGlobal(ctx, opts.Sess, opts.AgentID)
		if mErr != nil {
			return nil, mErr
		}
		id := agent.ID
		agentID = &id
	}

	now := time.Now().UTC()
	conversation := &orchestrator.Conversation{
		ID:          uuid.NewString(),
		WorkspaceID: opts.WorkspaceID,
		AgentID:     agentID,
		Type:        opts.Type,
		Title:       "",
		UnreadCount: 0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if mErr := s.conversationRepo.Create(ctx, opts.Sess, conversation); mErr != nil {
		return nil, mErr
	}

	return conversation, nil
}

// DeleteConversation removes a conversation from a workspace.
// Verifies workspace ownership before performing the deletion.
func (s *service) DeleteConversation(ctx context.Context, opts *orchestratorservice.DeleteConversationOpts) *merrors.Error {
	if opts == nil || opts.Sess == nil {
		return merrors.ErrInvalidInput
	}
	if strings.TrimSpace(opts.UserID) == "" || strings.TrimSpace(opts.WorkspaceID) == "" || strings.TrimSpace(opts.ConversationID) == "" {
		return merrors.ErrInvalidInput
	}

	// Verify workspace ownership.
	if _, mErr := s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, opts.WorkspaceID); mErr != nil {
		return mErr
	}

	return s.conversationRepo.Delete(ctx, opts.Sess, opts.WorkspaceID, opts.ConversationID)
}

// MarkConversationRead resets the unread count for a conversation to zero.
// Verifies workspace ownership before updating.
func (s *service) MarkConversationRead(ctx context.Context, opts *orchestratorservice.MarkConversationReadOpts) *merrors.Error {
	if opts == nil || opts.Sess == nil {
		return merrors.ErrInvalidInput
	}

	if _, mErr := s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, opts.WorkspaceID); mErr != nil {
		return mErr
	}

	return s.conversationRepo.MarkAsRead(ctx, opts.Sess, opts.WorkspaceID, opts.ConversationID)
}

// attachConversationData enriches a conversation with agent info and last message.
func (s *service) attachConversationData(ctx context.Context, sess *dbr.Session, conversation *orchestrator.Conversation, agentByID map[string]*orchestrator.Agent) {
	if conversation == nil {
		return
	}
	if conversation.AgentID != nil {
		conversation.Agent = agentByID[*conversation.AgentID]
	}
	lastMessage, mErr := s.messageRepo.GetLatestByConversationID(ctx, sess, conversation.ID)
	if mErr == nil && lastMessage != nil {
		if lastMessage.AgentID != nil {
			lastMessage.Agent = agentByID[*lastMessage.AgentID]
		}
		conversation.LastMessage = lastMessage
	}
}
