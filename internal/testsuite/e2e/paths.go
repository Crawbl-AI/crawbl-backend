package e2e

// URL path fragments used across orchestrator-API step definitions.
const (
	workspacesPath    = "/v1/workspaces/"
	conversationsPath = "/conversations/"
	messagesPath      = "/messages"
	agentsPath        = "/v1/agents/"
	memoriesPath      = "/memories"
)

// Repeated error/format strings.
const (
	errDBQueryFailed = "DB query failed: %w"
	errNoCurrentConv = "no current conversation set for %q"
)

// Common test fixture values.
const (
	berlinBuilderSlug = "berlin-builder"
	firstIDPath       = "data.0.id"
)
