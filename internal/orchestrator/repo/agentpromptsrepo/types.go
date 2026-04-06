// Package agentpromptsrepo provides PostgreSQL-based implementation of the AgentPromptsRepo interface.
// It handles persistence and retrieval of agent prompt entries.
package agentpromptsrepo

type agentPromptsRepo struct{}

var promptColumns = []string{
	"id",
	"agent_id",
	"name",
	"description",
	"content",
	"sort_order",
	"created_at",
	"updated_at",
}
