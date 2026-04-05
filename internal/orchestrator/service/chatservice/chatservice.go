package chatservice

import (
	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	agentclient "github.com/Crawbl-AI/crawbl-backend/internal/agent"
)

// New creates a new ChatService with the provided dependencies.
// db is required for background operations (e.g. pending message cleanup).
func New(
	db *dbr.Connection,
	repos Repos,
	runtimeClient agentclient.Client,
	broadcaster realtime.Broadcaster,
) orchestratorservice.ChatService {
	if db == nil {
		panic("chat service db cannot be nil")
	}
	if repos.Workspace == nil {
		panic("chat service workspace repo cannot be nil")
	}
	if repos.Agent == nil {
		panic("chat service agent repo cannot be nil")
	}
	if repos.Conversation == nil {
		panic("chat service conversation repo cannot be nil")
	}
	if repos.Message == nil {
		panic("chat service message repo cannot be nil")
	}
	if repos.Tools == nil {
		panic("chat service tools repo cannot be nil")
	}
	if repos.AgentSettings == nil {
		panic("chat service agent settings repo cannot be nil")
	}
	if repos.AgentPrompts == nil {
		panic("chat service agent prompts repo cannot be nil")
	}
	if repos.AgentHistory == nil {
		panic("chat service agent history repo cannot be nil")
	}
	if runtimeClient == nil {
		panic("chat service runtime client cannot be nil")
	}
	if broadcaster == nil {
		broadcaster = realtime.NopBroadcaster{}
	}

	return &service{
		db:                db,
		workspaceRepo:     repos.Workspace,
		agentRepo:         repos.Agent,
		conversationRepo:  repos.Conversation,
		messageRepo:       repos.Message,
		toolsRepo:         repos.Tools,
		agentSettingsRepo: repos.AgentSettings,
		agentPromptsRepo:  repos.AgentPrompts,
		agentHistoryRepo:  repos.AgentHistory,
		runtimeClient:     runtimeClient,
		broadcaster:       broadcaster,
		defaultAgents:     append([]orchestrator.DefaultAgentBlueprint(nil), orchestrator.DefaultAgents...),
	}
}
