package agentservice

import (
	"errors"

	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// New creates a new AgentService with the provided dependencies, returning an
// error if any required dependency is nil.
func New(
	repos Repos,
	runtimeClient userswarmclient.Client,
) (orchestratorservice.AgentService, error) {
	if repos.Workspace == nil {
		return nil, errors.New("agentservice: workspace repo is required")
	}
	if repos.Agent == nil {
		return nil, errors.New("agentservice: agent repo is required")
	}
	if repos.Tools == nil {
		return nil, errors.New("agentservice: tools repo is required")
	}
	if repos.AgentSettings == nil {
		return nil, errors.New("agentservice: agent settings repo is required")
	}
	if repos.AgentPrompts == nil {
		return nil, errors.New("agentservice: agent prompts repo is required")
	}
	if repos.AgentHistory == nil {
		return nil, errors.New("agentservice: agent history repo is required")
	}
	if runtimeClient == nil {
		return nil, errors.New("agentservice: runtime client is required")
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
	}, nil
}

// MustNew wraps New and panics on dependency-validation errors. Intended for
// use from main/init paths where misconfiguration is unrecoverable.
func MustNew(
	repos Repos,
	runtimeClient userswarmclient.Client,
) orchestratorservice.AgentService {
	svc, err := New(repos, runtimeClient)
	if err != nil {
		panic(err)
	}
	return svc
}
