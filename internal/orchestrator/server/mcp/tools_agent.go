package mcp

import (
	"context"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mcp/v1"
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

func newCreateAgentHistoryHandler(deps *Deps) sdkmcp.ToolHandlerFor[createAgentHistoryInput, *mcpv1.CreateAgentHistoryOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, _, workspaceID string, input createAgentHistoryInput) (*sdkmcp.CallToolResult, *mcpv1.CreateAgentHistoryOutput, error) {
		if input.Title == "" {
			return nil, &mcpv1.CreateAgentHistoryOutput{Info: "title is required"}, nil
		}

		RecordAPICall(ctx, "DB:INSERT agent_history")

		// Fall back to the runtime-propagated conversation ID so the
		// LLM does not have to remember it; the runtime stamps it on
		// every per-turn ctx via WithConversationID.
		if input.ConversationID == "" {
			input.ConversationID = conversationIDFromContext(ctx)
		}

		err := deps.MCPService.CreateAgentHistory(ctx, sess, workspaceID, &mcpservice.CreateAgentHistoryParams{
			AgentId:        input.AgentID,
			AgentSlug:      input.AgentSlug,
			ConversationId: input.ConversationID,
			Title:          input.Title,
			Subtitle:       input.Subtitle,
		})
		if err != nil {
			return nil, &mcpv1.CreateAgentHistoryOutput{Info: err.Error()}, nil
		}

		return nil, &mcpv1.CreateAgentHistoryOutput{Created: true, Info: "history entry created"}, nil
	})
}
