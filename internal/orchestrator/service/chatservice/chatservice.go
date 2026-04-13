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

// NewServiceOpts groups all dependencies for New and MustNew. DB is required;
// Broadcaster defaults to NopBroadcaster when nil. MemoryStack, PricingCache,
// UsagePublisher, and IngestPool are optional — nil values disable the
// respective feature cleanly.
type NewServiceOpts struct {
	DB             *dbr.Connection
	Repos          Repos
	RuntimeClient  userswarmclient.Client
	Broadcaster    realtime.Broadcaster
	MemoryStack    layers.Stack
	PricingCache   *pricing.Cache
	UsagePublisher *queue.UsagePublisher
	IngestPool     autoingest.Service
}

// New creates a new ChatService from the provided opts.
// Returns an error if any required dependency is nil.
func New(opts NewServiceOpts) (*Service, error) {
	if opts.DB == nil {
		return nil, errors.New("chatservice: db is required")
	}
	if opts.Repos.Workspace == nil {
		return nil, errors.New("chatservice: Workspace repo is required")
	}
	if opts.Repos.Agent == nil {
		return nil, errors.New("chatservice: Agent repo is required")
	}
	if opts.Repos.Conversation == nil {
		return nil, errors.New("chatservice: Conversation repo is required")
	}
	if opts.Repos.Message == nil {
		return nil, errors.New("chatservice: Message repo is required")
	}
	if opts.Repos.Tools == nil {
		return nil, errors.New("chatservice: Tools repo is required")
	}
	if opts.Repos.AgentSettings == nil {
		return nil, errors.New("chatservice: AgentSettings repo is required")
	}
	if opts.Repos.AgentPrompts == nil {
		return nil, errors.New("chatservice: AgentPrompts repo is required")
	}
	if opts.Repos.AgentHistory == nil {
		return nil, errors.New("chatservice: AgentHistory repo is required")
	}
	if opts.Repos.Usage == nil {
		return nil, errors.New("chatservice: Usage repo is required")
	}
	if opts.RuntimeClient == nil {
		return nil, errors.New("chatservice: runtimeClient is required")
	}
	if opts.Broadcaster == nil {
		opts.Broadcaster = realtime.NopBroadcaster{}
	}

	return &Service{
		db:                opts.DB,
		workspaceRepo:     opts.Repos.Workspace,
		agentRepo:         opts.Repos.Agent,
		conversationRepo:  opts.Repos.Conversation,
		messageRepo:       opts.Repos.Message,
		toolsRepo:         opts.Repos.Tools,
		agentSettingsRepo: opts.Repos.AgentSettings,
		agentPromptsRepo:  opts.Repos.AgentPrompts,
		agentHistoryRepo:  opts.Repos.AgentHistory,
		usageRepo:         opts.Repos.Usage,
		runtimeClient:     opts.RuntimeClient,
		broadcaster:       opts.Broadcaster,
		defaultAgents:     orchestrator.GetDefaultAgents(),
		memoryStack:       opts.MemoryStack,
		pricingCache:      opts.PricingCache,
		usagePublisher:    opts.UsagePublisher,
		ingestPool:        opts.IngestPool,
	}, nil
}

// MustNew creates a new ChatService or panics if any required dependency is nil.
// Use in main/wiring only; prefer New in code that can propagate errors.
func MustNew(opts NewServiceOpts) *Service {
	s, err := New(opts)
	if err != nil {
		panic(err)
	}
	return s
}
