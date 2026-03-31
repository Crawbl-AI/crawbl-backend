// Package agentrepo provides PostgreSQL-based implementation of the AgentRepo interface.
// It handles all database operations related to agent entities within workspaces.
package agentrepo

// agentRepo is the PostgreSQL implementation of the AgentRepo interface.
// It handles agent data persistence and retrieval operations.
type agentRepo struct{}

// agentColumns defines the column names used in SELECT queries for the agents table.
// These columns map directly to the AgentRow struct fields.
var agentColumns = []string{
	"id",
	"workspace_id",
	"name",
	"role",
	"slug",
	"avatar_url",
	"system_prompt",
	"sort_order",
	"created_at",
	"updated_at",
}
