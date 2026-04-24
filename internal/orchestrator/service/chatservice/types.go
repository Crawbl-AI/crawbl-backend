// Package chatservice implements the orchestrator chat service.
package chatservice

import (
	"context"
	"sync"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/autoingest"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/messagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/pricing"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// sendMessageFunc is the signature RespondToQuestions uses to dispatch the
// synthesized follow-up text message. Keeping it as a field (defaulting to
// Service.SendMessage) lets tests inject a stub without pulling the entire
// streaming pipeline into the unit-test harness.
type sendMessageFunc func(ctx context.Context, opts *orchestratorservice.SendMessageOpts) ([]*orchestrator.Message, *merrors.Error)

// Repos groups the repository dependencies used by the chat service.
// Passing a single struct instead of 8 individual parameters keeps the
// constructor signature clean and makes adding new repos a one-line change.
//
// Fields are typed against consumer-side interfaces (ports.go) so callers
// can satisfy the struct with any backend that provides the exact method
// subset chatservice needs — no coupling to a producer-owned interface.
type Repos struct {
	Workspace     workspaceGetter
	Agent         agentStore
	Conversation  conversationStore
	Message       messageStore
	Tools         toolsSeeder
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
	workspaceRepo     workspaceGetter
	agentRepo         agentStore
	conversationRepo  conversationStore
	messageRepo       messageStore
	toolsRepo         toolsSeeder
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
	// sendMessageFunc is the follow-up message dispatcher used by
	// RespondToQuestions. Defaults to Service.SendMessage in production; tests
	// inject a fake to simulate success/failure paths without running the
	// full streaming pipeline.
	sendMessageFn sendMessageFunc
}

// workspaceGetter is the workspace subset chatservice uses: ownership
// lookups and workspace seed.
type workspaceGetter interface {
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
}

// agentStore is the agent subset chatservice uses during bootstrap,
// conversation listing, and delegation routing.
type agentStore interface {
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error)
	GetByIDGlobal(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestrator.Agent, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, agent *orchestrator.Agent, sortOrder int) *merrors.Error
}

// conversationStore is the conversation subset chatservice uses across
// list, get, create, delete, mark-read, and default-swarm bootstrap.
type conversationStore interface {
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Conversation, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) (*orchestrator.Conversation, *merrors.Error)
	FindDefaultSwarm(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) (*orchestrator.Conversation, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, conversation *orchestrator.Conversation) *merrors.Error
	Create(ctx context.Context, sess orchestratorrepo.SessionRunner, conversation *orchestrator.Conversation) *merrors.Error
	Delete(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) *merrors.Error
	MarkAsRead(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) *merrors.Error
}

// messageStore is the message subset chatservice uses: persistence,
// pagination, context windows, delegation bookkeeping, and stale-pending
// cleanup.
type messageStore interface {
	ListByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, opts *orchestratorrepo.ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error)
	GetLatestByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string) (*orchestrator.Message, *merrors.Error)
	GetLatestByConversationIDs(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationIDs []string) (map[string]*orchestrator.Message, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string) (*orchestrator.Message, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, message *orchestrator.Message) *merrors.Error
	UpdateStatus(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string, status orchestrator.MessageStatus) *merrors.Error
	DeleteByID(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string) *merrors.Error
	ListRecent(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string, limit int) ([]*orchestrator.Message, *merrors.Error)
	RecordDelegation(ctx context.Context, sess orchestratorrepo.SessionRunner, opts messagerepo.RecordDelegationOpts) *merrors.Error
	CompleteDelegation(ctx context.Context, sess orchestratorrepo.SessionRunner, triggerMsgID, delegateAgentID string) *merrors.Error
	UpdateDelegationSummary(ctx context.Context, sess orchestratorrepo.SessionRunner, triggerMsgID, summary string) *merrors.Error
	UpdateToolState(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID, state string) *merrors.Error
}

// toolsSeeder is the tool-catalog subset chatservice uses to seed
// per-workspace tool defaults during first-run bootstrap.
type toolsSeeder interface {
	Seed(ctx context.Context, sess orchestratorrepo.SessionRunner, tools []orchestratorrepo.ToolRow) *merrors.Error
}

// agentSettingsStore is the agent_settings subset chatservice uses to
// seed default settings rows for newly provisioned blueprints.
type agentSettingsStore interface {
	GetByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestratorrepo.AgentSettingsRow, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentSettingsRow) *merrors.Error
}

// agentPromptsStore is the agent_prompts subset chatservice uses to
// seed default prompt rows for newly provisioned blueprints.
type agentPromptsStore interface {
	ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) ([]orchestratorrepo.AgentPromptRow, *merrors.Error)
	BulkSave(ctx context.Context, sess orchestratorrepo.SessionRunner, rows []orchestratorrepo.AgentPromptRow) *merrors.Error
}

// agentHistoryStore is the agent_history subset chatservice holds.
// The chat-service itself does not call into it today, but it is wired
// through the Repos struct because sibling services (agentservice) and
// future call sites in this package will consume it.
type agentHistoryStore interface {
	ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string, limit, offset int) ([]orchestratorrepo.AgentHistoryRow, *merrors.Error)
	CountByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error)
	Create(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentHistoryRow) *merrors.Error
}
