// Package agentrepo provides PostgreSQL-based implementation of the AgentRepo interface.
// It handles all database operations related to agent entities within workspaces.
package agentrepo

// agentColumns defines the column names used in SELECT queries for the agents table.
// These columns map directly to the AgentRow struct fields.
var agentColumns = []string{
	"id",
	"workspace_id",
	"name",
	"role",
	"avatar_url",
	"sort_order",
	"created_at",
	"updated_at",
}
