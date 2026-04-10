// Package agentservice provides the agent service implementation for handling
// agent details, settings, tools, and history retrieval. It enriches agent
// records with runtime status from the user swarm and verifies workspace
// ownership before returning data.
package agentservice

import (
	memrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// Repos groups the repository dependencies used by the agent service.
// Passing a single struct instead of 6 individual parameters keeps the
// constructor signature clean and makes adding new repos a one-line change.
type Repos struct {
	Workspace     orchestratorrepo.WorkspaceRepo
	Agent         orchestratorrepo.AgentRepo
	Tools         orchestratorrepo.ToolsRepo
	AgentSettings orchestratorrepo.AgentSettingsRepo
	AgentPrompts  orchestratorrepo.AgentPromptsRepo
	AgentHistory  orchestratorrepo.AgentHistoryRepo
	Usage         usagerepo.Repo
	Drawer        memrepo.DrawerRepo
}

// service implements the orchestratorservice.AgentService interface.
type service struct {
	workspaceRepo     orchestratorrepo.WorkspaceRepo
	agentRepo         orchestratorrepo.AgentRepo
	toolsRepo         orchestratorrepo.ToolsRepo
	agentSettingsRepo orchestratorrepo.AgentSettingsRepo
	agentPromptsRepo  orchestratorrepo.AgentPromptsRepo
	agentHistoryRepo  orchestratorrepo.AgentHistoryRepo
	runtimeClient     userswarmclient.Client
	usageRepo         usagerepo.Repo
	drawerRepo        memrepo.DrawerRepo
}
