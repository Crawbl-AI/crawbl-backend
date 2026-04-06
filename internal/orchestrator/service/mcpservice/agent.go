package mcpservice

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
)

func (s *service) ResolveAgentBySlug(ctx contextT, sess sessionT, workspaceID, slug string) (string, error) {
	return s.resolveAgentID(ctx, sess, workspaceID, slug)
}

func (s *service) CreateAgentHistory(ctx contextT, sess sessionT, workspaceID string, params *CreateAgentHistoryParams) error {
	var agentID string
	if params.AgentID != "" {
		agentID = params.AgentID
	} else if params.AgentSlug != "" {
		var err error
		agentID, err = s.resolveAgentID(ctx, sess, workspaceID, params.AgentSlug)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("agent_id or agent_slug is required")
	}

	var convID *string
	if params.ConversationID != "" {
		convID = &params.ConversationID
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
		return fmt.Errorf("create history entry: %s", mErr.Error())
	}
	return nil
}
