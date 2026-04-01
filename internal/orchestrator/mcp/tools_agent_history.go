package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
)

func newCreateAgentHistoryHandler(deps *Deps) sdkmcp.ToolHandlerFor[createAgentHistoryInput, createAgentHistoryOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input createAgentHistoryInput) (*sdkmcp.CallToolResult, createAgentHistoryOutput, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, createAgentHistoryOutput{}, fmt.Errorf("unauthorized: no user identity")
		}

		if input.AgentSlug == "" || input.Title == "" {
			return nil, createAgentHistoryOutput{Info: "agent_slug and title are required"}, nil
		}

		sess := deps.newSession()

		// Look up agent by slug + workspaceID
		RecordAPICall(ctx, "DB:SELECT agents WHERE workspace_id="+workspaceID+" AND slug="+input.AgentSlug)
		agents, mErr := deps.AgentRepo.ListByWorkspaceID(ctx, sess, workspaceID)
		if mErr != nil {
			return nil, createAgentHistoryOutput{Info: "failed to look up agents: " + mErr.Error()}, nil
		}

		var agentID string
		for _, a := range agents {
			if a.Slug == input.AgentSlug {
				agentID = a.ID
				break
			}
		}
		if agentID == "" {
			return nil, createAgentHistoryOutput{Info: "agent not found with slug: " + input.AgentSlug}, nil
		}

		// Create history entry
		RecordAPICall(ctx, "DB:INSERT agent_history agent_id="+agentID)
		var convID *string
		if input.ConversationID != "" {
			convID = &input.ConversationID
		}

		row := &orchestratorrepo.AgentHistoryRow{
			ID:             uuid.NewString(),
			AgentID:        agentID,
			ConversationID: convID,
			Title:          input.Title,
			Subtitle:       input.Subtitle,
			CreatedAt:      time.Now(),
		}

		if mErr := deps.AgentHistoryRepo.Create(ctx, sess, row); mErr != nil {
			return nil, createAgentHistoryOutput{Info: "failed to create history entry: " + mErr.Error()}, nil
		}

		return nil, createAgentHistoryOutput{Created: true, Info: "history entry created for agent " + input.AgentSlug}, nil
	}
}
