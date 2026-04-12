// Package agentservice provides the agent service implementation for handling
// agent details, settings, tools, and history retrieval. It enriches agent
// records with runtime status from the user swarm and verifies workspace
// ownership before returning data.
package agentservice

import (
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// Repos groups the repository dependencies used by the agent service.
// Passing a single struct instead of 6 individual parameters keeps the
// constructor signature clean and makes adding new repos a one-line change.
//
// Fields are typed against consumer-side interfaces (ports.go) so that
// callers can satisfy the struct with any implementation providing the
// exact method subset agentservice uses — no coupling to producer
// packages beyond their concrete structs.
type Repos struct {
	Workspace     workspaceStore
	Agent         agentStore
	Tools         toolsStore
	AgentSettings agentSettingsStore
	AgentPrompts  agentPromptsStore
	AgentHistory  agentHistoryStore
	Usage         usagerepo.Repo
	Drawer        drawerStore
}

// Service implements agent-specific operations: agent details, settings,
// tools, and history retrieval. Consumers depend on their own
// consumer-side interfaces (e.g. handler.agentPort) per the project's
// "interfaces at consumer" convention.
type Service struct {
	workspaceRepo     workspaceStore
	agentRepo         agentStore
	toolsRepo         toolsStore
	agentSettingsRepo agentSettingsStore
	agentPromptsRepo  agentPromptsStore
	agentHistoryRepo  agentHistoryStore
	runtimeClient     userswarmclient.Client
	usageRepo         usagerepo.Repo
	drawerRepo        drawerStore
}
