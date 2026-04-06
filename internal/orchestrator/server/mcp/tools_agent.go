package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
)

type createAgentHistoryInput struct {
	AgentID        string `json:"agent_id,omitempty"`
	AgentSlug      string `json:"agent_slug,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle,omitempty"`
}

type createAgentHistoryOutput struct {
	Created bool   `json:"created"`
	Info    string `json:"info"`
}

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
		RecordAPICall(ctx, "DB:INSERT agent_history")

		err := deps.MCPService.CreateAgentHistory(ctx, sess, workspaceID, &mcpservice.CreateAgentHistoryParams{
			AgentID:        input.AgentID,
			AgentSlug:      input.AgentSlug,
			ConversationID: input.ConversationID,
			Title:          input.Title,
			Subtitle:       input.Subtitle,
		})
		if err != nil {
			return nil, createAgentHistoryOutput{Info: err.Error()}, nil
		}

		return nil, createAgentHistoryOutput{Created: true, Info: "history entry created"}, nil
	}
}
