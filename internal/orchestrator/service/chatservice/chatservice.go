package chatservice

import (
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// New creates a new ChatService with the provided dependencies.
func New(
	workspaceRepo workspaceRepo,
	agentRepo agentRepo,
	conversationRepo conversationRepo,
	messageRepo messageRepo,
	toolsRepo toolsRepo,
	agentSettingsRepo agentSettingsRepo,
	agentPromptsRepo agentPromptsRepo,
	agentHistoryRepo agentHistoryRepo,
	runtimeClient userswarmclient.Client,
	broadcaster realtime.Broadcaster,
) orchestratorservice.ChatService {
	if workspaceRepo == nil {
		panic("chat service workspace repo cannot be nil")
	}
	if agentRepo == nil {
		panic("chat service agent repo cannot be nil")
	}
	if conversationRepo == nil {
		panic("chat service conversation repo cannot be nil")
	}
	if messageRepo == nil {
		panic("chat service message repo cannot be nil")
	}
	if toolsRepo == nil {
		panic("chat service tools repo cannot be nil")
	}
	if agentSettingsRepo == nil {
		panic("chat service agent settings repo cannot be nil")
	}
	if agentPromptsRepo == nil {
		panic("chat service agent prompts repo cannot be nil")
	}
	if agentHistoryRepo == nil {
		panic("chat service agent history repo cannot be nil")
	}
	if runtimeClient == nil {
		panic("chat service runtime client cannot be nil")
	}
	if broadcaster == nil {
		broadcaster = realtime.NopBroadcaster{}
	}

	return &service{
		workspaceRepo:     workspaceRepo,
		agentRepo:         agentRepo,
		conversationRepo:  conversationRepo,
		messageRepo:       messageRepo,
		toolsRepo:         toolsRepo,
		agentSettingsRepo: agentSettingsRepo,
		agentPromptsRepo:  agentPromptsRepo,
		agentHistoryRepo:  agentHistoryRepo,
		runtimeClient:     runtimeClient,
		broadcaster:       broadcaster,
		defaultAgents:     append([]orchestrator.DefaultAgentBlueprint(nil), orchestrator.DefaultAgents...),
	}
}
