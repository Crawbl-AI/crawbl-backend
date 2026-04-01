package chatservice

import (
	"context"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/google/uuid"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// SendMessage sends a user message and returns the agent's reply.
// Agent routing: conversation's agent > first @-mentioned agent > default.
func (s *service) SendMessage(ctx context.Context, opts *orchestratorservice.SendMessageOpts) (*orchestrator.Message, *merrors.Error) {
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

	// For display: attribute the reply to an agent even for swarm messages.
	// The Manager delegates to sub-agents internally; we surface the primary
	// agent as the responder since the webhook doesn't report which sub-agent
	// actually handled the request.
	displayResponder := responder
	if displayResponder == nil && len(agents) > 0 {
		displayResponder = agents[0]
	}

	// Signal agent is processing
	if displayResponder != nil {
		s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, displayResponder.ID, string(orchestrator.AgentStatusBusy))
		s.broadcaster.EmitAgentTyping(ctx, opts.WorkspaceID, conversation.ID, displayResponder.ID, true)
	}

	// Call ZeroClaw — use the original responder for webhook routing.
	// nil means the Manager (base agent) handles the message.
	sendOpts := &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   opts.Content.Text,
		SessionID: conversation.ID,
	}
	if responder != nil {
		sendOpts.AgentID = responder.Slug
	}
	replyText, mErr := s.runtimeClient.SendText(ctx, sendOpts)

	// Signal agent is done
	if displayResponder != nil {
		s.broadcaster.EmitAgentTyping(ctx, opts.WorkspaceID, conversation.ID, displayResponder.ID, false)
		s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, displayResponder.ID, string(orchestrator.AgentStatusOnline))
	}

	if mErr != nil {
		if displayResponder != nil {
			s.broadcaster.EmitAgentStatus(ctx, opts.WorkspaceID, displayResponder.ID, string(orchestrator.AgentStatusError))
		}
		return nil, mErr
	}

	// Persist messages — use displayResponder so swarm replies have agent metadata.
	return s.persistMessagePair(ctx, opts, conversation, agents, displayResponder, replyText)
}

// persistMessagePair saves the user message + agent reply in a transaction.
func (s *service) persistMessagePair(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agents []*orchestrator.Agent,
	responder *orchestrator.Agent,
	replyText string,
) (*orchestrator.Message, *merrors.Error) {
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

	agentByID := mapAgentsByID(agents)
	if replyAgentID != nil {
		replyMessage.Agent = agentByID[*replyAgentID]
	}
	if userMessage.AgentID != nil {
		userMessage.Agent = agentByID[*userMessage.AgentID]
	}

	s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, userMessage)
	s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, replyMessage)

	return replyMessage, nil
}

// stringPtr returns a pointer to a trimmed string, or nil if empty.
func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
