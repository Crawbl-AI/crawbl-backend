package chatservice

import (
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	runtimeclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// New creates a new ChatService with the provided dependencies.
func New(
	workspaceRepo workspaceRepo,
	agentRepo agentRepo,
	conversationRepo conversationRepo,
	messageRepo messageRepo,
	runtimeClient runtimeclient.Client,
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
	if runtimeClient == nil {
		panic("chat service runtime client cannot be nil")
	}
	if broadcaster == nil {
		broadcaster = realtime.NopBroadcaster{}
	}

	return &service{
		workspaceRepo:    workspaceRepo,
		agentRepo:        agentRepo,
		conversationRepo: conversationRepo,
		messageRepo:      messageRepo,
		runtimeClient:    runtimeClient,
		broadcaster:      broadcaster,
		defaultAgents:    append([]orchestrator.DefaultAgentBlueprint(nil), orchestrator.DefaultAgents...),
	}
}
