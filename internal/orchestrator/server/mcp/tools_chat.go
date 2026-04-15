package mcp

import (
	"context"
	"time"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
)

type listConversationsInput struct {
	IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"include archived conversations"`
	Description     string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type listConversationsOutput struct {
	Conversations []conversationBrief `json:"conversations"`
}

type conversationBrief struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Type      string  `json:"type"`
	AgentID   *string `json:"agent_id,omitempty"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func newListConversationsHandler(deps *Deps) sdkmcp.ToolHandlerFor[listConversationsInput, listConversationsOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, _ listConversationsInput) (*sdkmcp.CallToolResult, listConversationsOutput, error) {
		conversations, err := deps.MCPService.ListConversations(ctx, sess, userID, workspaceID)
		if err != nil {
			return nil, listConversationsOutput{}, err
		}

		briefs := make([]conversationBrief, 0, len(conversations))
		for _, c := range conversations {
			briefs = append(briefs, conversationBrief{
				ID:        c.ID,
				Title:     c.Title,
				Type:      string(c.Type),
				AgentID:   c.AgentID,
				CreatedAt: c.CreatedAt.Format(time.RFC3339),
				UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
			})
		}

		return nil, listConversationsOutput{Conversations: briefs}, nil
	})
}

type searchMessagesInput struct {
	ConversationID string `json:"conversation_id" jsonschema:"ID of the conversation to search in"`
	Query          string `json:"query" jsonschema:"search keyword or phrase"`
	Limit          int    `json:"limit" jsonschema:"maximum results to return (default 20, max 50)"`
	Description    string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type searchMessagesOutput struct {
	Messages []messageBrief `json:"messages"`
	Count    int            `json:"count"`
}

type messageBrief struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

func newSearchMessagesHandler(deps *Deps) sdkmcp.ToolHandlerFor[searchMessagesInput, searchMessagesOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input searchMessagesInput) (*sdkmcp.CallToolResult, searchMessagesOutput, error) {
		limit := input.Limit
		if limit <= 0 {
			limit = defaultSearchLimit
		}
		if limit > maxSearchLimit {
			limit = maxSearchLimit
		}

		results, err := deps.MCPService.SearchMessages(ctx, sess, userID, workspaceID, input.ConversationID, input.Query, limit)
		if err != nil {
			return nil, searchMessagesOutput{}, err
		}

		briefs := make([]messageBrief, 0, len(results))
		for _, r := range results {
			briefs = append(briefs, messageBrief{
				ID:        r.ID,
				Role:      r.Role,
				Text:      r.Text,
				CreatedAt: r.CreatedAt.Format(time.RFC3339),
			})
		}

		return nil, searchMessagesOutput{Messages: briefs, Count: len(briefs)}, nil
	})
}

type sendMessageInput struct {
	AgentSlug      string `json:"agent_slug" jsonschema:"slug of the target agent (e.g. 'wally', 'eve')"`
	Message        string `json:"message" jsonschema:"the message/task to send to the target agent"`
	ConversationID string `json:"conversation_id,omitempty" jsonschema:"optional conversation ID for context"`
	Description    string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type sendMessageOutput struct {
	Success   bool   `json:"success"`
	AgentSlug string `json:"agent_slug"`
	Response  string `json:"response"`
	MessageID string `json:"message_id"`
	Error     string `json:"error,omitempty"`
}

func newSendMessageHandler(deps *Deps) sdkmcp.ToolHandlerFor[sendMessageInput, sendMessageOutput] {
	return authedToolWithUser(deps, func(ctx context.Context, sess *dbr.Session, userID, workspaceID string, input sendMessageInput) (*sdkmcp.CallToolResult, sendMessageOutput, error) {
		RecordAPICall(ctx, "RUNTIME:GRPC Converse")

		// Fall back to the runtime-propagated conversation ID. The
		// active conversation is the natural context for an A2A
		// hand-off; making the LLM repeat it from input was an
		// avoidable failure mode.
		if input.ConversationID == "" {
			input.ConversationID = conversationIDFromContext(ctx)
		}

		result, err := deps.MCPService.SendMessageToAgent(ctx, sess, &mcpservice.SendAgentMessageParams{
			UserID:         userID,
			WorkspaceID:    workspaceID,
			SessionID:      sessionIDFromContext(ctx),
			AgentSlug:      input.AgentSlug,
			Message:        input.Message,
			ConversationID: input.ConversationID,
		})
		if err != nil {
			return nil, sendMessageOutput{}, err
		}

		return nil, sendMessageOutput{
			Success:   result.Success,
			AgentSlug: result.AgentSlug,
			Response:  result.Response,
			MessageID: result.MessageID,
			Error:     result.Error,
		}, nil
	})
}
