package chatservice

import (
	"errors"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/autoingest"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/pricing"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// New creates a new ChatService with the provided dependencies.
// db is required for request sessions. memoryStack may be nil; when nil,
// context building falls back to recent messages only. ingestPool may
// be nil, in which case auto-ingest is disabled cleanly.
// Returns an error if any required dependency is nil.
func New(
	db *dbr.Connection,
	repos Repos,
	runtimeClient userswarmclient.Client,
	broadcaster realtime.Broadcaster,
	memoryStack layers.Stack,
	pricingCache *pricing.Cache,
	usagePublisher *queue.UsagePublisher,
	ingestPool autoingest.Service,
) (*Service, error) {
	if db == nil {
		return nil, errors.New("chatservice: db is required")
	}
	if repos.Workspace == nil {
		return nil, errors.New("chatservice: Workspace repo is required")
	}
	if repos.Agent == nil {
		return nil, errors.New("chatservice: Agent repo is required")
	}
	if repos.Conversation == nil {
		return nil, errors.New("chatservice: Conversation repo is required")
	}
	if repos.Message == nil {
		return nil, errors.New("chatservice: Message repo is required")
	}
	if repos.Tools == nil {
		return nil, errors.New("chatservice: Tools repo is required")
	}
	if repos.AgentSettings == nil {
		return nil, errors.New("chatservice: AgentSettings repo is required")
	}
	if repos.AgentPrompts == nil {
		return nil, errors.New("chatservice: AgentPrompts repo is required")
	}
	if repos.AgentHistory == nil {
		return nil, errors.New("chatservice: AgentHistory repo is required")
	}
	if repos.Usage == nil {
		return nil, errors.New("chatservice: Usage repo is required")
	}
	if runtimeClient == nil {
		return nil, errors.New("chatservice: runtimeClient is required")
	}
	if broadcaster == nil {
		broadcaster = realtime.NopBroadcaster{}
	}

	return &Service{
		db:                db,
		workspaceRepo:     repos.Workspace,
		agentRepo:         repos.Agent,
		conversationRepo:  repos.Conversation,
		messageRepo:       repos.Message,
		toolsRepo:         repos.Tools,
		agentSettingsRepo: repos.AgentSettings,
		agentPromptsRepo:  repos.AgentPrompts,
		agentHistoryRepo:  repos.AgentHistory,
		usageRepo:         repos.Usage,
		runtimeClient:     runtimeClient,
		broadcaster:       broadcaster,
		defaultAgents:     orchestrator.GetDefaultAgents(),
		memoryStack:       memoryStack,
		pricingCache:      pricingCache,
		usagePublisher:    usagePublisher,
		ingestPool:        ingestPool,
	}, nil
}

// MustNew creates a new ChatService or panics if any required dependency is nil.
// Use in main/wiring only; prefer New in code that can propagate errors.
func MustNew(
	db *dbr.Connection,
	repos Repos,
	runtimeClient userswarmclient.Client,
	broadcaster realtime.Broadcaster,
	memoryStack layers.Stack,
	pricingCache *pricing.Cache,
	usagePublisher *queue.UsagePublisher,
	ingestPool autoingest.Service,
) *Service {
	s, err := New(db, repos, runtimeClient, broadcaster, memoryStack, pricingCache, usagePublisher, ingestPool)
	if err != nil {
		panic(err)
	}
	return s
}
