// Package chatservice implements the orchestrator chat service.
package chatservice

import (
	"sync"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/autoingest"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/pricing"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// Repos groups the repository dependencies used by the chat service.
// Passing a single struct instead of 8 individual parameters keeps the
// constructor signature clean and makes adding new repos a one-line change.
//
// Fields are typed against consumer-side interfaces (ports.go) so callers
// can satisfy the struct with any backend that provides the exact method
// subset chatservice needs — no coupling to a producer-owned interface.
type Repos struct {
	Workspace     workspaceStore
	Agent         agentStore
	Conversation  conversationStore
	Message       messageStore
	Tools         toolsStore
	AgentSettings agentSettingsStore
	AgentPrompts  agentPromptsStore
	AgentHistory  agentHistoryStore
	Usage         usagerepo.Repo
}

// Service implements chat operations (conversations, messages, agents, streaming).
// Consumers depend on their own consumer-side interfaces (e.g. handler.chatPort)
// per the project's "interfaces at consumer" convention.
type Service struct {
	db                *dbr.Connection
	workspaceRepo     workspaceStore
	agentRepo         agentStore
	conversationRepo  conversationStore
	messageRepo       messageStore
	toolsRepo         toolsStore
	agentSettingsRepo agentSettingsStore
	agentPromptsRepo  agentPromptsStore
	agentHistoryRepo  agentHistoryStore
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
