package chatservice

import (
	"context"

	"github.com/gocraft/dbr/v2"

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
	for _, conversation := range conversations {
		s.attachConversationData(ctx, opts.Sess, conversation, agentByID)
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
