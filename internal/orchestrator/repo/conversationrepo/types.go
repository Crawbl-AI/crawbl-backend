// Package conversationrepo provides PostgreSQL-based implementation of the ConversationRepo interface.
// It handles all database operations related to conversation entities within workspaces.
package conversationrepo

// conversationColumns defines the column names used in SELECT queries for the conversations table.
// These columns map directly to the ConversationRow struct fields.
var conversationColumns = []string{
	"id",
	"workspace_id",
	"agent_id",
	"type",
	"title",
	"unread_count",
	"created_at",
	"updated_at",
}
