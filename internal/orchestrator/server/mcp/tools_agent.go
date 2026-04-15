package mcp

import (
	"context"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
)

type createAgentHistoryInput struct {
	AgentID        string `json:"agent_id,omitempty"`
	AgentSlug      string `json:"agent_slug,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle,omitempty"`
	Description    string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type createAgentHistoryOutput struct {
	Created bool   `json:"created"`
	Info    string `json:"info"`
}

func newCreateAgentHistoryHandler(deps *Deps) sdkmcp.ToolHandlerFor[createAgentHistoryInput, createAgentHistoryOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, _, workspaceID string, input createAgentHistoryInput) (*sdkmcp.CallToolResult, createAgentHistoryOutput, error) {
		if input.Title == "" {
			return nil, createAgentHistoryOutput{Info: "title is required"}, nil
		}

		RecordAPICall(ctx, "DB:INSERT agent_history")

		// Fall back to the runtime-propagated conversation ID so the
		// LLM does not have to remember it; the runtime stamps it on
		// every per-turn ctx via WithConversationID.
		if input.ConversationID == "" {
			input.ConversationID = conversationIDFromContext(ctx)
		}

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
	})
}
