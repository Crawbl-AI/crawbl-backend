package agentservice

import (
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	agentclient "github.com/Crawbl-AI/crawbl-backend/internal/agent"
)

// New creates a new AgentService with the provided dependencies.
func New(
	workspaceRepo workspaceRepo,
	agentRepo agentRepo,
	toolsRepo toolsRepo,
	agentSettingsRepo agentSettingsRepo,
	agentPromptsRepo agentPromptsRepo,
	agentHistoryRepo agentHistoryRepo,
	runtimeClient agentclient.Client,
) orchestratorservice.AgentService {
	if workspaceRepo == nil {
		panic("agent service workspace repo cannot be nil")
	}
	if agentRepo == nil {
		panic("agent service agent repo cannot be nil")
	}
	if toolsRepo == nil {
		panic("agent service tools repo cannot be nil")
	}
	if agentSettingsRepo == nil {
		panic("agent service agent settings repo cannot be nil")
	}
	if agentPromptsRepo == nil {
		panic("agent service agent prompts repo cannot be nil")
	}
	if agentHistoryRepo == nil {
		panic("agent service agent history repo cannot be nil")
	}
	if runtimeClient == nil {
		panic("agent service runtime client cannot be nil")
	}

	return &service{
		workspaceRepo:     workspaceRepo,
		agentRepo:         agentRepo,
		toolsRepo:         toolsRepo,
		agentSettingsRepo: agentSettingsRepo,
		agentPromptsRepo:  agentPromptsRepo,
		agentHistoryRepo:  agentHistoryRepo,
		runtimeClient:     runtimeClient,
	}
}
