package e2e

const (
	pathWorkspaces        = "/v1/workspaces/"
	pathConversations     = "/conversations/"
	pathAgents            = "/v1/agents/"
	pathMessages          = "/messages"
	pathMemories          = "/memories"
	errDBQueryFailed      = "DB query failed: %w"
	errNoConversation     = "no current conversation set for %q"
	whereWorkspaceIDIn    = "workspace_id IN ?"
	testUserBerlinBuilder = "berlin-builder"
	jsonPathFirstID       = "data.0.id"
)
