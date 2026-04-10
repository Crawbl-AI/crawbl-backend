// Package chatservice implements the orchestrator chat service.
package chatservice

import (
	"sync"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/autoingest"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/layers"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
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
	usagePublisher    *queue.UsagePublisher
	// ingestPool is the in-process auto-ingest Service. Nil disables
	// auto-ingest cleanly.
	ingestPool autoingest.Service
	// bootstrappedWorkspaces caches workspace IDs that have already been
	// bootstrapped in this process. The value is always struct{}{}. This
	// eliminates redundant seed queries on every read path (ListConversations,
	// GetConversation, ListMessages, SendMessage). The cache is process-local
	// and intentionally lost on pod restart — the first request per workspace
	// per pod pays the bootstrap cost once, which is acceptable.
	bootstrappedWorkspaces sync.Map
}
