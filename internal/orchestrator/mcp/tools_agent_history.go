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

		if input.Title == "" {
			return nil, createAgentHistoryOutput{Info: "title is required"}, nil
		}

		sess := deps.newSession()

		// Resolve agent: prefer UUID fast path, fall back to slug lookup.
		var agentID string
		if input.AgentID != "" {
			agentID = input.AgentID
		} else if input.AgentSlug != "" {
			RecordAPICall(ctx, "DB:SELECT agents WHERE workspace_id="+workspaceID+" AND slug="+input.AgentSlug)
			var resolveErr error
			agentID, resolveErr = resolveAgentBySlug(ctx, deps, sess, workspaceID, input.AgentSlug)
			if resolveErr != nil {
				return nil, createAgentHistoryOutput{Info: resolveErr.Error()}, nil
			}
		} else {
			return nil, createAgentHistoryOutput{Info: "agent_id or agent_slug is required"}, nil
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

		return nil, createAgentHistoryOutput{Created: true, Info: "history entry created for agent " + agentID}, nil
	}
}
