package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newListConversationsHandler creates the MCP handler for listing conversations in the authenticated workspace.
func newListConversationsHandler(deps *Deps) sdkmcp.ToolHandlerFor[listConversationsInput, listConversationsOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ listConversationsInput) (*sdkmcp.CallToolResult, listConversationsOutput, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, listConversationsOutput{}, fmt.Errorf("unauthorized")
		}

		sess := deps.newSession()

		// Verify workspace ownership first.
		if _, mErr := deps.WorkspaceRepo.GetByID(ctx, sess, userID, workspaceID); mErr != nil {
			return nil, listConversationsOutput{}, fmt.Errorf("workspace not found")
		}

		conversations, mErr := deps.ConversationRepo.ListByWorkspaceID(ctx, sess, workspaceID)
		if mErr != nil {
			return nil, listConversationsOutput{}, fmt.Errorf("failed to list conversations")
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
	}
}

// newSearchMessagesHandler creates the MCP handler for full-text search over past messages.
func newSearchMessagesHandler(deps *Deps) sdkmcp.ToolHandlerFor[searchMessagesInput, searchMessagesOutput] {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest, input searchMessagesInput) (*sdkmcp.CallToolResult, searchMessagesOutput, error) {
		userID := userIDFromContext(ctx)
		workspaceID := workspaceIDFromContext(ctx)
		if userID == "" || workspaceID == "" {
			return nil, searchMessagesOutput{}, fmt.Errorf("unauthorized")
		}

		sess := deps.newSession()

		// Verify workspace ownership.
		if _, mErr := deps.WorkspaceRepo.GetByID(ctx, sess, userID, workspaceID); mErr != nil {
			return nil, searchMessagesOutput{}, fmt.Errorf("workspace not found")
		}

		// Verify conversation belongs to this workspace.
		if _, mErr := deps.ConversationRepo.GetByID(ctx, sess, workspaceID, input.ConversationID); mErr != nil {
			return nil, searchMessagesOutput{}, fmt.Errorf("conversation not found in this workspace")
		}

		limit := input.Limit
		if limit <= 0 {
			limit = defaultSearchLimit
		}
		if limit > maxSearchLimit {
			limit = maxSearchLimit
		}

		// Search messages by content text. The content column is JSONB;
		// we search the text representation for the query string.
		// This is scoped to a verified conversation in the user's workspace.
		query := "%" + sanitizeLike(input.Query) + "%"

		var rows []messageRow
		_, err := sess.Select("id", "role", "content::text as content", "created_at").
			From("messages").
			Where("conversation_id = ?", input.ConversationID).
			Where("content::text ILIKE ?", query).
			OrderDir("created_at", false).
			Limit(uint64(limit)).
			LoadContext(ctx, &rows)
		if err != nil {
			return nil, searchMessagesOutput{}, fmt.Errorf("search failed")
		}

		briefs := make([]messageBrief, 0, len(rows))
		for _, r := range rows {
			// Extract the text field from content JSON.
			text := extractTextFromContent(r.Content)
			briefs = append(briefs, messageBrief{
				ID:        r.ID,
				Role:      r.Role,
				Text:      truncateStr(text, agentContextMaxTextLen),
				CreatedAt: r.CreatedAt.Format(time.RFC3339),
			})
		}

		return nil, searchMessagesOutput{Messages: briefs, Count: len(briefs)}, nil
	}
}

// sanitizeLike escapes LIKE wildcards in user input to prevent injection.
func sanitizeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// extractTextFromContent pulls the "text" field from a JSON content string.
func extractTextFromContent(content string) string {
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return ""
	}
	return parsed.Text
}
