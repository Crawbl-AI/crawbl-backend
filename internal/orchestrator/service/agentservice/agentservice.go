package agentservice

import (
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// New creates a new AgentService with the provided dependencies.
func New(
	repos Repos,
	runtimeClient userswarmclient.Client,
) orchestratorservice.AgentService {
	if repos.Workspace == nil {
		panic("agent service workspace repo cannot be nil")
	}
	if repos.Agent == nil {
		panic("agent service agent repo cannot be nil")
	}
	if repos.Tools == nil {
		panic("agent service tools repo cannot be nil")
	}
	if repos.AgentSettings == nil {
		panic("agent service agent settings repo cannot be nil")
	}
	if repos.AgentPrompts == nil {
		panic("agent service agent prompts repo cannot be nil")
	}
	if repos.AgentHistory == nil {
		panic("agent service agent history repo cannot be nil")
	}
	if runtimeClient == nil {
		panic("agent service runtime client cannot be nil")
	}

	return &service{
		workspaceRepo:     repos.Workspace,
		agentRepo:         repos.Agent,
		toolsRepo:         repos.Tools,
		agentSettingsRepo: repos.AgentSettings,
		agentPromptsRepo:  repos.AgentPrompts,
		agentHistoryRepo:  repos.AgentHistory,
		runtimeClient:     runtimeClient,
		usageRepo:         repos.Usage,
		drawerRepo:        repos.Drawer,
	}
}
