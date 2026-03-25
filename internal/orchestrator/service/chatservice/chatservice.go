package chatservice

import (
	"context"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/runtimeclient"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

func New(
	workspaceRepo workspaceRepo,
	agentRepo agentRepo,
	conversationRepo conversationRepo,
	messageRepo messageRepo,
	runtimeClient runtimeclient.Client,
) orchestratorservice.ChatService {
	if workspaceRepo == nil {
		panic("chat service workspace repo cannot be nil")
	}
	if agentRepo == nil {
		panic("chat service agent repo cannot be nil")
	}
	if conversationRepo == nil {
		panic("chat service conversation repo cannot be nil")
	}
	if messageRepo == nil {
		panic("chat service message repo cannot be nil")
	}
	if runtimeClient == nil {
		panic("chat service runtime client cannot be nil")
	}

	return &service{
		workspaceRepo:    workspaceRepo,
		agentRepo:        agentRepo,
		conversationRepo: conversationRepo,
		messageRepo:      messageRepo,
		runtimeClient:    runtimeClient,
		defaultAgents:    append([]orchestrator.DefaultAgentBlueprint(nil), orchestrator.DefaultAgents...),
	}
}

func (s *service) ListAgents(ctx context.Context, opts *orchestratorservice.ListAgentsOpts) ([]*orchestrator.Agent, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	workspace, agents, _, mErr := s.ensureWorkspaceBootstrap(ctx, opts.Sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	runtimeState, mErr := s.runtimeClient.EnsureRuntime(ctx, &runtimeclient.EnsureRuntimeOpts{
		UserID:          workspace.UserID,
		WorkspaceID:     workspace.ID,
		WaitForVerified: false,
	})
	if mErr != nil {
		return nil, mErr
	}

	for _, agent := range agents {
		agent.Status = statusForRuntime(runtimeState)
		agent.HasUpdate = false
	}

	return agents, nil
}

func (s *service) ListConversations(ctx context.Context, opts *orchestratorservice.ListConversationsOpts) ([]*orchestrator.Conversation, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	_, agents, conversations, mErr := s.ensureWorkspaceBootstrap(ctx, opts.Sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	agentByID := mapAgentsByID(agents)
	for _, conversation := range conversations {
		s.attachConversationData(ctx, opts.Sess, conversation, agentByID)
	}

	return conversations, nil
}

func (s *service) GetConversation(ctx context.Context, opts *orchestratorservice.GetConversationOpts) (*orchestrator.Conversation, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	_, agents, _, mErr := s.ensureWorkspaceBootstrap(ctx, opts.Sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	conversation, mErr := s.conversationRepo.GetByID(ctx, opts.Sess, opts.WorkspaceID, opts.ConversationID)
	if mErr != nil {
		return nil, mErr
	}

	s.attachConversationData(ctx, opts.Sess, conversation, mapAgentsByID(agents))
	return conversation, nil
}

func (s *service) ListMessages(ctx context.Context, opts *orchestratorservice.ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	_, agents, _, mErr := s.ensureWorkspaceBootstrap(ctx, opts.Sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

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

func (s *service) SendMessage(ctx context.Context, opts *orchestratorservice.SendMessageOpts) (*orchestrator.Message, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}
	if opts.Content.Type != orchestrator.MessageContentTypeText || strings.TrimSpace(opts.Content.Text) == "" || len(opts.Attachments) > 0 {
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

	runtimeState, mErr := s.runtimeClient.EnsureRuntime(ctx, &runtimeclient.EnsureRuntimeOpts{
		UserID:          workspace.UserID,
		WorkspaceID:     workspace.ID,
		WaitForVerified: true,
	})
	if mErr != nil {
		return nil, mErr
	}

	replyText, mErr := s.runtimeClient.SendText(ctx, &runtimeclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   opts.Content.Text,
		SessionID: conversation.ID,
	})
	if mErr != nil {
		return nil, mErr
	}

	now := time.Now().UTC()
	replyAt := now.Add(time.Millisecond)
	var replyAgentID *string
	if responder := defaultResponderAgent(agents); responder != nil {
		replyAgentID = &responder.ID
	}

	userMessage := &orchestrator.Message{
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
	replyMessage := &orchestrator.Message{
		ID:             uuid.NewString(),
		ConversationID: conversation.ID,
		Role:           orchestrator.MessageRoleAgent,
		Content: orchestrator.MessageContent{
			Type: orchestrator.MessageContentTypeText,
			Text: replyText,
		},
		Status:      orchestrator.MessageStatusDelivered,
		AgentID:     replyAgentID,
		Attachments: []orchestrator.Attachment{},
		CreatedAt:   replyAt,
		UpdatedAt:   replyAt,
	}

	conversation.UpdatedAt = replyAt
	conversation.LastMessage = replyMessage

	if _, mErr := database.WithTransaction(opts.Sess, "send chat message", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
		if mErr := s.messageRepo.Save(ctx, tx, userMessage); mErr != nil {
			return nil, mErr
		}
		if mErr := s.messageRepo.Save(ctx, tx, replyMessage); mErr != nil {
			return nil, mErr
		}
		if mErr := s.conversationRepo.Save(ctx, tx, conversation); mErr != nil {
			return nil, mErr
		}
		return replyMessage, nil
	}); mErr != nil {
		return nil, mErr
	}

	if replyAgentID != nil {
		replyMessage.Agent = mapAgentsByID(agents)[*replyAgentID]
	}

	return replyMessage, nil
}

func (s *service) ensureWorkspaceBootstrap(ctx context.Context, sess *dbr.Session, userID, workspaceID string) (*orchestrator.Workspace, []*orchestrator.Agent, []*orchestrator.Conversation, *merrors.Error) {
	workspace, mErr := s.workspaceRepo.GetByID(ctx, sess, userID, workspaceID)
	if mErr != nil {
		return nil, nil, nil, mErr
	}

	agents, mErr := s.ensureDefaultAgents(ctx, sess, workspace)
	if mErr != nil {
		return nil, nil, nil, mErr
	}

	conversations, mErr := s.ensureDefaultConversations(ctx, sess, workspace)
	if mErr != nil {
		return nil, nil, nil, mErr
	}

	return workspace, agents, conversations, nil
}

func (s *service) ensureDefaultAgents(ctx context.Context, sess *dbr.Session, workspace *orchestrator.Workspace) ([]*orchestrator.Agent, *merrors.Error) {
	agents, mErr := s.agentRepo.ListByWorkspaceID(ctx, sess, workspace.ID)
	if mErr != nil {
		return nil, mErr
	}

	agentsByRole := make(map[string]*orchestrator.Agent, len(agents))
	for _, agent := range agents {
		agentsByRole[agent.Role] = agent
	}

	missing := false
	for _, blueprint := range s.defaultAgents {
		if agentsByRole[blueprint.Role] == nil {
			missing = true
			break
		}
	}
	if !missing {
		return agents, nil
	}

	createdAgents, mErr := database.WithTransaction(sess, "ensure default agents", func(tx *dbr.Tx) ([]*orchestrator.Agent, *merrors.Error) {
		freshAgents, mErr := s.agentRepo.ListByWorkspaceID(ctx, tx, workspace.ID)
		if mErr != nil {
			return nil, mErr
		}

		freshByRole := make(map[string]*orchestrator.Agent, len(freshAgents))
		for _, agent := range freshAgents {
			freshByRole[agent.Role] = agent
		}

		now := time.Now().UTC()
		for idx, blueprint := range s.defaultAgents {
			agent := freshByRole[blueprint.Role]
			if agent == nil {
				agent = &orchestrator.Agent{
					ID:          uuid.NewString(),
					WorkspaceID: workspace.ID,
					Name:        blueprint.Name,
					Role:        blueprint.Role,
					AvatarURL:   orchestrator.DefaultAgentAvatarURL,
					CreatedAt:   now,
					UpdatedAt:   now,
				}
			} else {
				agent.Name = blueprint.Name
				agent.Role = blueprint.Role
				agent.AvatarURL = orchestrator.DefaultAgentAvatarURL
				agent.UpdatedAt = now
			}

			if mErr := s.agentRepo.Save(ctx, tx, agent, idx); mErr != nil {
				return nil, mErr
			}
			freshByRole[blueprint.Role] = agent
		}

		freshAgents, mErr = s.agentRepo.ListByWorkspaceID(ctx, tx, workspace.ID)
		if mErr != nil {
			return nil, mErr
		}
		return freshAgents, nil
	})
	if mErr != nil {
		return nil, mErr
	}

	return createdAgents, nil
}

func (s *service) ensureDefaultConversations(ctx context.Context, sess *dbr.Session, workspace *orchestrator.Workspace) ([]*orchestrator.Conversation, *merrors.Error) {
	conversations, mErr := s.conversationRepo.ListByWorkspaceID(ctx, sess, workspace.ID)
	if mErr != nil {
		return nil, mErr
	}

	for _, conversation := range conversations {
		if conversation.Type == orchestrator.ConversationTypeSwarm {
			return conversations, nil
		}
	}

	createdConversations, mErr := database.WithTransaction(sess, "ensure default conversations", func(tx *dbr.Tx) ([]*orchestrator.Conversation, *merrors.Error) {
		if _, mErr := s.conversationRepo.FindDefaultSwarm(ctx, tx, workspace.ID); mErr == nil {
			return s.conversationRepo.ListByWorkspaceID(ctx, tx, workspace.ID)
		} else if !merrors.IsCode(mErr, merrors.ErrCodeConversationNotFound) {
			return nil, mErr
		}

		now := time.Now().UTC()
		conversation := &orchestrator.Conversation{
			ID:          uuid.NewString(),
			WorkspaceID: workspace.ID,
			Type:        orchestrator.ConversationTypeSwarm,
			Title:       orchestrator.DefaultSwarmTitle,
			UnreadCount: 0,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if mErr := s.conversationRepo.Save(ctx, tx, conversation); mErr != nil {
			return nil, mErr
		}
		return s.conversationRepo.ListByWorkspaceID(ctx, tx, workspace.ID)
	})
	if mErr != nil {
		return nil, mErr
	}

	return createdConversations, nil
}

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

func mapAgentsByID(agents []*orchestrator.Agent) map[string]*orchestrator.Agent {
	indexed := make(map[string]*orchestrator.Agent, len(agents))
	for _, agent := range agents {
		if agent != nil {
			indexed[agent.ID] = agent
		}
	}
	return indexed
}

func defaultResponderAgent(agents []*orchestrator.Agent) *orchestrator.Agent {
	if len(agents) == 0 {
		return nil
	}
	return agents[0]
}

func statusForRuntime(runtimeState *orchestrator.RuntimeStatus) orchestrator.AgentStatus {
	if runtimeState == nil {
		return orchestrator.AgentStatusOffline
	}
	if runtimeState.Verified {
		return orchestrator.AgentStatusOnline
	}
	switch strings.ToLower(strings.TrimSpace(runtimeState.Phase)) {
	case "progressing", "pending":
		return orchestrator.AgentStatusBusy
	default:
		return orchestrator.AgentStatusOffline
	}
}

func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
