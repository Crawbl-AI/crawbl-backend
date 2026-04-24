package mcp

import (
	"context"
	"time"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mcp/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
)

func newListConversationsHandler(deps *Deps) sdkmcp.ToolHandlerFor[listConversationsInput, *mcpv1.ListConversationsOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, _ listConversationsInput) (*sdkmcp.CallToolResult, *mcpv1.ListConversationsOutput, error) {
		conversations, err := deps.MCPService.ListConversations(ctx, sess, userID, workspaceID)
		if err != nil {
			return nil, nil, err
		}

		briefs := make([]*mcpv1.ConversationBrief, 0, len(conversations))
		for _, c := range conversations {
			briefs = append(briefs, &mcpv1.ConversationBrief{
				Id:        c.ID,
				Title:     c.Title,
				Type:      string(c.Type),
				AgentId:   c.AgentID,
				CreatedAt: c.CreatedAt.Format(time.RFC3339),
				UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
			})
		}

		return nil, &mcpv1.ListConversationsOutput{Conversations: briefs}, nil
	})
}

func newSearchMessagesHandler(deps *Deps) sdkmcp.ToolHandlerFor[searchMessagesInput, *mcpv1.SearchMessagesOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input searchMessagesInput) (*sdkmcp.CallToolResult, *mcpv1.SearchMessagesOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = defaultSearchLimit
		}
		if limit > maxSearchLimit {
			limit = maxSearchLimit
		}

		results, err := deps.MCPService.SearchMessages(ctx, sess, mcpservice.SearchMessagesOpts{
			UserID:         userID,
			WorkspaceID:    workspaceID,
			ConversationID: input.ConversationID,
			Query:          input.Query,
			Limit:          limit,
		})
		if err != nil {
			return nil, nil, err
		}

		briefs := make([]*mcpv1.ToolMessageBrief, 0, len(results))
		for i := range results {
			briefs = append(briefs, &mcpv1.ToolMessageBrief{
				Id:        results[i].Id,
				Role:      results[i].Role,
				Text:      results[i].Text,
				CreatedAt: results[i].CreatedAt.AsTime().Format(time.RFC3339),
			})
		}

		return nil, &mcpv1.SearchMessagesOutput{Messages: briefs, Count: int32(len(briefs))}, nil
	})
}

func newSendMessageHandler(deps *Deps) sdkmcp.ToolHandlerFor[sendMessageInput, *mcpv1.SendMessageToolOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input sendMessageInput) (*sdkmcp.CallToolResult, *mcpv1.SendMessageToolOutput, error) {
		RecordAPICall(ctx, "RUNTIME:GRPC Converse")

		// Fall back to the runtime-propagated conversation ID. The
		// active conversation is the natural context for an A2A
		// hand-off; making the LLM repeat it from input was an
		// avoidable failure mode.
		if input.ConversationID == "" {
			input.ConversationID = conversationIDFromContext(ctx)
		}

		result, err := deps.MCPService.SendMessageToAgent(ctx, sess, &mcpservice.SendAgentMessageParams{
			UserId:         userID,
			WorkspaceId:    workspaceID,
			SessionId:      sessionIDFromContext(ctx),
			AgentSlug:      input.AgentSlug,
			Message:        input.Message,
			ConversationId: input.ConversationID,
		})
		if err != nil {
			return nil, nil, err
		}

		return nil, &mcpv1.SendMessageToolOutput{
			Success:   result.Success,
			AgentSlug: result.AgentSlug,
			Response:  result.Response,
			MessageId: result.MessageId,
			Error:     result.Error,
		}, nil
	})
}
