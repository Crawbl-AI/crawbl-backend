package mcpservice

import (
	"time"

	"github.com/google/uuid"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

func (s *service) ResolveAgentBySlug(ctx contextT, sess sessionT, workspaceID, slug string) (string, error) {
	return s.resolveAgentID(ctx, sess, workspaceID, slug)
}

func (s *service) CreateAgentHistory(ctx contextT, sess sessionT, workspaceID string, params *CreateAgentHistoryParams) error {
	var agentID string
	if params.AgentId != "" {
		// Verify the agent belongs to the HMAC-scoped workspace before using the
		// caller-supplied ID. Without this check a valid token for workspace A could
		// write history rows for an agent in workspace B by knowing its UUID.
		agent, mErr := s.repos.Agent.GetByIDGlobal(ctx, sess, params.AgentId)
		if mErr != nil {
			return merrors.ErrAgentNotFound
		}
		if agent.WorkspaceID != workspaceID {
			return merrors.ErrAgentNotFound
		}
		agentID = params.AgentId
	} else if params.AgentSlug != "" {
		var err error
		agentID, err = s.resolveAgentID(ctx, sess, workspaceID, params.AgentSlug)
		if err != nil {
			return err
		}
	} else {
		return merrors.NewServerErrorText("agent_id or agent_slug is required")
	}

	var convID *string
	if params.ConversationId != "" {
		convID = &params.ConversationId
	}

	row := &orchestratorrepo.AgentHistoryRow{
		ID:             uuid.NewString(),
		AgentID:        agentID,
		ConversationID: convID,
		Title:          params.Title,
		Subtitle:       params.Subtitle,
		CreatedAt:      time.Now(),
	}

	if mErr := s.repos.AgentHistory.Create(ctx, sess, row); mErr != nil {
		return merrors.WrapServerError(mErr, "create history entry")
	}
	return nil
}
