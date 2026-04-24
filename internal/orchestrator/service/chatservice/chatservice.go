package chatservice

import (
	"errors"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/realtime"
)

// New creates a new ChatService with the provided dependencies.
// DB is required for request sessions. MemoryStack may be nil; when nil,
// context building falls back to recent messages only. IngestPool may
// be nil, in which case auto-ingest is disabled cleanly.
// Returns an error if any required dependency is nil.
func New(deps Deps) (*Service, error) {
	if deps.DB == nil {
		return nil, errors.New("chatservice: db is required")
	}
	if deps.Repos.Workspace == nil {
		return nil, errors.New("chatservice: Workspace repo is required")
	}
	if deps.Repos.Agent == nil {
		return nil, errors.New("chatservice: Agent repo is required")
	}
	if deps.Repos.Conversation == nil {
		return nil, errors.New("chatservice: Conversation repo is required")
	}
	if deps.Repos.Message == nil {
		return nil, errors.New("chatservice: Message repo is required")
	}
	if deps.Repos.Tools == nil {
		return nil, errors.New("chatservice: Tools repo is required")
	}
	if deps.Repos.AgentSettings == nil {
		return nil, errors.New("chatservice: AgentSettings repo is required")
	}
	if deps.Repos.AgentPrompts == nil {
		return nil, errors.New("chatservice: AgentPrompts repo is required")
	}
	if deps.Repos.AgentHistory == nil {
		return nil, errors.New("chatservice: AgentHistory repo is required")
	}
	if deps.Repos.Usage == nil {
		return nil, errors.New("chatservice: Usage repo is required")
	}
	if deps.RuntimeClient == nil {
		return nil, errors.New("chatservice: runtimeClient is required")
	}
	if deps.Broadcaster == nil {
		deps.Broadcaster = realtime.NopBroadcaster{}
	}

	return &Service{
		db:                deps.DB,
		workspaceRepo:     deps.Repos.Workspace,
		agentRepo:         deps.Repos.Agent,
		conversationRepo:  deps.Repos.Conversation,
		messageRepo:       deps.Repos.Message,
		toolsRepo:         deps.Repos.Tools,
		agentSettingsRepo: deps.Repos.AgentSettings,
		agentPromptsRepo:  deps.Repos.AgentPrompts,
		agentHistoryRepo:  deps.Repos.AgentHistory,
		usageRepo:         deps.Repos.Usage,
		runtimeClient:     deps.RuntimeClient,
		broadcaster:       deps.Broadcaster,
		defaultAgents:     orchestrator.GetDefaultAgents(),
		memoryStack:       deps.MemoryStack,
		pricingCache:      deps.PricingCache,
		usagePublisher:    deps.UsagePublisher,
		ingestPool:        deps.IngestPool,
	}, nil
}

// MustNew creates a new ChatService or panics if any required dependency is nil.
// Use in main/wiring only; prefer New in code that can propagate errors.
func MustNew(deps Deps) *Service {
	s, err := New(deps)
	if err != nil {
		panic(err)
	}
	return s
}
