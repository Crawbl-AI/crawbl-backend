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
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// New creates a new ChatService instance with the provided dependencies.
// All repository and client parameters must be non-nil; the function will panic
// if any required dependency is nil.
//
// The returned service handles workspace bootstrapping, default agent provisioning,
// conversation management, and message operations for the chat subsystem.
func New(
	workspaceRepo workspaceRepo,
	agentRepo agentRepo,
	conversationRepo conversationRepo,
	messageRepo messageRepo,
	runtimeClient runtimeclient.Client,
	broadcaster realtime.Broadcaster,
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
	if broadcaster == nil {
		broadcaster = realtime.NopBroadcaster{}
	}

	return &service{
		workspaceRepo:    workspaceRepo,
		agentRepo:        agentRepo,
		conversationRepo: conversationRepo,
		messageRepo:      messageRepo,
		runtimeClient:    runtimeClient,
		broadcaster:      broadcaster,
		defaultAgents:    append([]orchestrator.DefaultAgentBlueprint(nil), orchestrator.DefaultAgents...),
	}
}

// ListAgents retrieves all agents for a workspace after ensuring the workspace
// is properly bootstrapped with default agents. Each agent's status is updated
// based on the current runtime state.
//
// Returns an error if the session is nil, the workspace cannot be found,
// or runtime state cannot be retrieved.
func (s *service) ListAgents(ctx context.Context, opts *orchestratorservice.ListAgentsOpts) ([]*orchestrator.Agent, *merrors.Error) {
	if opts == nil || opts.Sess == nil {
		return nil, merrors.ErrInvalidInput
	}

	workspace, agents, _, mErr := s.ensureWorkspaceBootstrap(ctx, opts.Sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	s.enrichAgentStatus(ctx, workspace, agents)
	for _, agent := range agents {
		agent.HasUpdate = false
	}

	return agents, nil
}

// ListConversations retrieves all conversations for a workspace after ensuring
// the workspace is bootstrapped. Each conversation is enriched with agent
// information and the latest message.
//
// Returns an error if the session is nil, the workspace cannot be found,
// or the conversations cannot be retrieved.
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

// GetConversation retrieves a specific conversation by ID after ensuring the
// workspace is bootstrapped. The conversation is enriched with agent information
// and the latest message.
//
// Returns an error if the session is nil, the workspace or conversation
// cannot be found, or the conversation ID is invalid.
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

// ListMessages retrieves a paginated list of messages for a specific conversation.
// Messages are enriched with agent information where applicable.
//
// Returns an error if the session is nil, the workspace or conversation
// cannot be found, or the messages cannot be retrieved.
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

// SendMessage sends a user message to a conversation and returns the agent's reply.
// The function ensures the workspace is bootstrapped, verifies the runtime is ready,
// sends the message to the swarm runtime, and persists both the user message
// and agent reply to storage.
//
// Currently, only text messages without attachments are supported.
// Returns an error if the message type is unsupported, the workspace or
// conversation cannot be found, the runtime is not ready, or persistence fails.
//
//nolint:cyclop
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

	for _, agent := range agents {
		agent.Status = statusForRuntime(runtimeState)
	}

	// Determine the responding agent — use conversation's agent if set, otherwise first agent.
	var responder *orchestrator.Agent
	if conversation.AgentID != nil {
		for _, agent := range agents {
			if agent.ID == *conversation.AgentID {
				responder = agent
				break
			}
		}
	}
	if responder == nil {
		responder = defaultResponderAgent(agents)
	}

	// Emit typing indicator before the runtime call so the mobile client shows feedback.
	if responder != nil {
		s.broadcaster.EmitAgentTyping(ctx, opts.WorkspaceID, conversation.ID, responder.ID, true)
	}

	sendOpts := &runtimeclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   opts.Content.Text,
		SessionID: conversation.ID,
	}
	if responder != nil {
		sendOpts.AgentID = responder.Role
		sendOpts.SystemPrompt = responder.SystemPrompt
	}
	replyText, mErr := s.runtimeClient.SendText(ctx, sendOpts)

	// Stop typing indicator regardless of success or failure.
	if responder != nil {
		s.broadcaster.EmitAgentTyping(ctx, opts.WorkspaceID, conversation.ID, responder.ID, false)
	}

	if mErr != nil {
		return nil, mErr
	}

	now := time.Now().UTC()
	replyAt := now.Add(time.Millisecond)
	var replyAgentID *string
	if responder != nil {
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

	// Emit real-time events after successful persistence so connected clients see updates.
	// Attach agent to user message if it has one, for consistent event payloads.
	if userMessage.AgentID != nil {
		userMessage.Agent = mapAgentsByID(agents)[*userMessage.AgentID]
	}
	s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, userMessage)
	s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, replyMessage)

	return replyMessage, nil
}

// ensureWorkspaceBootstrap ensures the workspace exists and is fully bootstrapped
// with default agents and conversations. It returns the workspace, all agents,
// and all conversations after ensuring everything is properly initialized.
//
// This is an internal helper method called by the public service methods
// to guarantee consistent workspace state before performing operations.
func (s *service) ensureWorkspaceBootstrap(ctx context.Context, sess *dbr.Session, userID, workspaceID string) (*orchestrator.Workspace, []*orchestrator.Agent, []*orchestrator.Conversation, *merrors.Error) {
	workspace, mErr := s.workspaceRepo.GetByID(ctx, sess, userID, workspaceID)
	if mErr != nil {
		return nil, nil, nil, mErr
	}

	agents, mErr := s.ensureDefaultAgents(ctx, sess, workspace)
	if mErr != nil {
		return nil, nil, nil, mErr
	}

	conversations, mErr := s.ensureDefaultConversations(ctx, sess, workspace, agents)
	if mErr != nil {
		return nil, nil, nil, mErr
	}

	return workspace, agents, conversations, nil
}

// ensureDefaultAgents ensures that all default agents defined in the defaultAgents
// blueprint exist for the given workspace. It creates any missing agents and
// updates existing agents to match the current blueprint definitions.
//
// The function uses a transaction to ensure atomic updates and returns all
// agents for the workspace after synchronization.
//
//nolint:cyclop
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
					ID:           uuid.NewString(),
					WorkspaceID:  workspace.ID,
					Name:         blueprint.Name,
					Role:         blueprint.Role,
					SystemPrompt: blueprint.SystemPrompt,
					AvatarURL:    orchestrator.DefaultAgentAvatarURL,
					CreatedAt:    now,
					UpdatedAt:    now,
				}
			} else {
				agent.Name = blueprint.Name
				agent.Role = blueprint.Role
				agent.SystemPrompt = blueprint.SystemPrompt
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

// ensureDefaultConversations ensures that a default swarm conversation and
// per-agent conversations exist for the workspace. Creates any that are missing.
func (s *service) ensureDefaultConversations(ctx context.Context, sess *dbr.Session, workspace *orchestrator.Workspace, agents []*orchestrator.Agent) ([]*orchestrator.Conversation, *merrors.Error) {
	conversations, mErr := s.conversationRepo.ListByWorkspaceID(ctx, sess, workspace.ID)
	if mErr != nil {
		return nil, mErr
	}

	// Check what's missing: swarm conversation + one per agent
	hasSwarm := false
	agentConvs := make(map[string]bool) // agent ID → has conversation
	for _, c := range conversations {
		if c.Type == orchestrator.ConversationTypeSwarm {
			hasSwarm = true
		}
		if c.AgentID != nil {
			agentConvs[*c.AgentID] = true
		}
	}

	allPresent := hasSwarm
	for _, agent := range agents {
		if !agentConvs[agent.ID] {
			allPresent = false
			break
		}
	}
	if allPresent {
		return conversations, nil
	}

	createdConversations, mErr := database.WithTransaction(sess, "ensure default conversations", func(tx *dbr.Tx) ([]*orchestrator.Conversation, *merrors.Error) {
		now := time.Now().UTC()

		// Create swarm conversation if missing
		if !hasSwarm {
			if _, findErr := s.conversationRepo.FindDefaultSwarm(ctx, tx, workspace.ID); findErr != nil {
				if !merrors.IsCode(findErr, merrors.ErrCodeConversationNotFound) {
					return nil, findErr
				}
				swarmConv := &orchestrator.Conversation{
					ID:          uuid.NewString(),
					WorkspaceID: workspace.ID,
					Type:        orchestrator.ConversationTypeSwarm,
					Title:       orchestrator.DefaultSwarmTitle,
					CreatedAt:   now,
					UpdatedAt:   now,
				}
				if mErr := s.conversationRepo.Save(ctx, tx, swarmConv); mErr != nil {
					return nil, mErr
				}
			}
		}

		// Create per-agent conversations if missing
		for _, agent := range agents {
			if agentConvs[agent.ID] {
				continue
			}
			agentID := agent.ID
			agentConv := &orchestrator.Conversation{
				ID:          uuid.NewString(),
				WorkspaceID: workspace.ID,
				AgentID:     &agentID,
				Type:        orchestrator.ConversationTypeAgent,
				Title:       agent.Name,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if mErr := s.conversationRepo.Save(ctx, tx, agentConv); mErr != nil {
				return nil, mErr
			}
		}

		return s.conversationRepo.ListByWorkspaceID(ctx, tx, workspace.ID)
	})
	if mErr != nil {
		return nil, mErr
	}

	return createdConversations, nil
}

// attachConversationData enriches a conversation with related data including
// agent information (if the conversation has an agent ID) and the latest message.
// This is a helper method to populate conversation details for API responses.
//
// If the conversation is nil, the function returns immediately without error.
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

// mapAgentsByID creates a lookup map from agent IDs to agent objects.
// This is a utility function for efficient agent lookup by ID.
func mapAgentsByID(agents []*orchestrator.Agent) map[string]*orchestrator.Agent {
	indexed := make(map[string]*orchestrator.Agent, len(agents))
	for _, agent := range agents {
		if agent != nil {
			indexed[agent.ID] = agent
		}
	}
	return indexed
}

// defaultResponderAgent returns the first agent in the list as the default
// responder for messages. Returns nil if the agent list is empty.
func defaultResponderAgent(agents []*orchestrator.Agent) *orchestrator.Agent {
	if len(agents) == 0 {
		return nil
	}
	return agents[0]
}

// enrichAgentStatus queries the workspace runtime and sets each agent's status
// based on the current runtime phase. If the runtime state cannot be retrieved,
// agents default to offline status.
func (s *service) enrichAgentStatus(ctx context.Context, workspace *orchestrator.Workspace, agents []*orchestrator.Agent) {
	runtimeState, mErr := s.runtimeClient.EnsureRuntime(ctx, &runtimeclient.EnsureRuntimeOpts{
		UserID:          workspace.UserID,
		WorkspaceID:     workspace.ID,
		WaitForVerified: false,
	})
	if mErr != nil {
		for _, agent := range agents {
			agent.Status = orchestrator.AgentStatusOffline
		}
		return
	}
	for _, agent := range agents {
		agent.Status = statusForRuntime(runtimeState)
	}
}

// statusForRuntime maps a runtime status to an agent status.
// Online status indicates the runtime is verified and ready.
// Busy status indicates the runtime is starting or progressing.
// Offline status indicates the runtime is not available or has failed.
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

// stringPtr returns a pointer to the trimmed string value, or nil if the
// trimmed value is empty. This is a utility function for handling optional
// string pointer fields.
func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
