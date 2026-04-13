package chatservice

import (
	"context"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// GetWorkspaceSummary retrieves aggregate workspace data: agent count and last message preview.
// Uses existing repo methods to avoid adding new query infrastructure.
func (s *Service) GetWorkspaceSummary(ctx context.Context, opts *orchestratorservice.GetWorkspaceSummaryOpts) (*orchestrator.WorkspaceSummary, *merrors.Error) {
	if opts == nil || strings.TrimSpace(opts.WorkspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}
	sess := database.SessionFromContext(ctx)

	agents, mErr := s.agentRepo.ListByWorkspaceID(ctx, sess, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	summary := &orchestrator.WorkspaceSummary{
		TotalAgents: len(agents),
	}

	// Find the most recently updated conversation to get the last message.
	// Conversations are returned ordered by updated_at DESC.
	conversations, mErr := s.conversationRepo.ListByWorkspaceID(ctx, sess, opts.WorkspaceID)
	if mErr != nil || len(conversations) == 0 {
		return summary, nil
	}

	latestMsg, mErr := s.messageRepo.GetLatestByConversationID(ctx, sess, conversations[0].ID)
	if mErr != nil {
		// No messages yet is normal for new workspaces.
		return summary, nil
	}

	senderName := orchestrator.UserSenderDisplayName
	if latestMsg.AgentID != nil {
		// Look up agent name from the already-fetched agents list.
		for _, agent := range agents {
			if agent.ID == *latestMsg.AgentID {
				senderName = agent.Name
				break
			}
		}
	}

	summary.LastMessagePreview = &orchestrator.LastMessagePreview{
		Text:       latestMsg.Content.Text,
		SenderName: senderName,
		Timestamp:  latestMsg.CreatedAt,
	}

	return summary, nil
}
