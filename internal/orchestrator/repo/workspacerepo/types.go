// Package workspacerepo provides PostgreSQL-based implementation of the WorkspaceRepo interface.
// It handles all database operations related to workspace entities.
package workspacerepo

// workspaceRepo is the PostgreSQL implementation of the WorkspaceRepo interface.
// It handles workspace data persistence and retrieval operations.
type workspaceRepo struct{}

// workspaceColumns defines the column names used in SELECT queries for the workspaces table.
// These columns map directly to the WorkspaceRow struct fields.
var workspaceColumns = []string{
	"id",
	"user_id",
	"name",
	"created_at",
	"updated_at",
}
