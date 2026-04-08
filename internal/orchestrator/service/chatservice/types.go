// Package chatservice provides the chat service implementation for handling
// agent listings, conversations, and message operations within user workspaces.
// It orchestrates workspace bootstrapping, default agent provisioning, and
// runtime communication for swarm-based chat interactions.
package chatservice

import (
	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/layers"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/usagepublisher"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/pricing"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// Repos groups the repository dependencies used by the chat service.
// Passing a single struct instead of 8 individual parameters keeps the
// constructor signature clean and makes adding new repos a one-line change.
type Repos struct {
	Workspace     orchestratorrepo.WorkspaceRepo
	Agent         orchestratorrepo.AgentRepo
	Conversation  orchestratorrepo.ConversationRepo
	Message       orchestratorrepo.MessageRepo
	Tools         orchestratorrepo.ToolsRepo
	AgentSettings orchestratorrepo.AgentSettingsRepo
	AgentPrompts  orchestratorrepo.AgentPromptsRepo
	AgentHistory  orchestratorrepo.AgentHistoryRepo
	Usage         usagerepo.Repo
}

// service implements the ChatService interface.
type service struct {
	db                *dbr.Connection
	workspaceRepo     orchestratorrepo.WorkspaceRepo
	agentRepo         orchestratorrepo.AgentRepo
	conversationRepo  orchestratorrepo.ConversationRepo
	messageRepo       orchestratorrepo.MessageRepo
	toolsRepo         orchestratorrepo.ToolsRepo
	agentSettingsRepo orchestratorrepo.AgentSettingsRepo
	agentPromptsRepo  orchestratorrepo.AgentPromptsRepo
	agentHistoryRepo  orchestratorrepo.AgentHistoryRepo
	usageRepo         usagerepo.Repo
	runtimeClient     userswarmclient.Client
	broadcaster       realtime.Broadcaster
	defaultAgents     []orchestrator.DefaultAgentBlueprint
	memoryStack       layers.Stack
	pricingCache      *pricing.Cache
	usagePublisher    *usagepublisher.Publisher
}
