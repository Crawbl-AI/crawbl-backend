// Package chatservice implements the orchestrator chat service.
package chatservice

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/autoingest"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/realtime"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/messagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// Inbound answer payload caps. Keeping these at the service boundary means
// oversized payloads are rejected before we mutate any persistence.
const maxCustomTextLen = 4096

// questionPrefix is the common prefix for per-question validation error messages.
const questionPrefix = "question "

// agentSilentResponse is the sentinel text agents return when they have nothing to say.
const agentSilentResponse = "[SILENT]"

// taskPreviewMaxRunes caps the delegation task_preview field.
const taskPreviewMaxRunes = 120

// sendMessageFunc is the signature RespondToQuestions uses to dispatch the
// synthesized follow-up text message. Keeping it as a field (defaulting to
// Service.SendMessage) lets tests inject a stub without pulling the entire
// streaming pipeline into the unit-test harness.
type sendMessageFunc func(ctx context.Context, opts *orchestratorservice.SendMessageOpts) ([]*orchestrator.Message, *merrors.Error)

// Deps holds the infrastructure dependencies for a ChatService.
// DB is required. Broadcaster defaults to a no-op when nil.
// MemoryStack, PricingCache, UsagePublisher, and IngestPool are optional
// and degrade gracefully when nil.
type Deps struct {
	DB             *dbr.Connection
	Repos          Repos
	RuntimeClient  userswarmclient.Client
	Broadcaster    realtime.Broadcaster
	MemoryStack    layers.Stack
	PricingCache   *queue.PricingCache
	UsagePublisher *queue.UsagePublisher
	IngestPool     autoingest.Service
}

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
	pricingCache      *queue.PricingCache
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

// agentLookups holds pre-built lookup maps for agents within a workspace.
type agentLookups struct {
	byID    map[string]*orchestrator.Agent
	bySlug  map[string]*orchestrator.Agent
	manager *orchestrator.Agent
}

// agentResult holds the outcome of a parallel agent call in swarm mode.
type agentResult struct {
	replies []*orchestrator.Message
	err     *merrors.Error
}

// pendingToolCall tracks a ToolCallEvent so we can resolve the tool name,
// agent, and parsed args when the matching ToolResultEvent arrives.
type pendingToolCall struct {
	tool      string       // e.g. agentruntimetools.ToolTransferToAgent
	agentSlug string       // agent slug from the ToolCallEvent (e.g. "manager")
	args      toolCallArgs // parsed args — reused on ToolResult for delegation resolution
	messageID string       // persisted tool_status message ID for updating on completion
}

// mapNamer is a layers.AgentNamer backed by a pre-built agent-by-ID map.
// It never performs a DB lookup — the map is populated once per request from
// the workspace roster, so AgentName is always O(1).
type mapNamer map[string]*orchestrator.Agent

// AgentName satisfies layers.AgentNamer using the in-memory lookup map.
func (m mapNamer) AgentName(_ context.Context, _ database.SessionRunner, agentID string) (string, bool) {
	if a, ok := m[agentID]; ok && a != nil {
		return a.Name, true
	}
	return "", false
}

// subAgentStream tracks a placeholder message and accumulated text for a single
// agent_id seen during a multi-agent streaming response (Phase 5).
type subAgentStream struct {
	agent       *orchestrator.Agent
	placeholder *orchestrator.Message
	accumulated strings.Builder
	chunkCount  int
	firstChunk  bool
	done        bool // received a StreamEventDone for this agent_id
}

// toolCallArgs is the result of parsing a tool call's raw JSON args once.
// Returned by parseToolCallArgs and consumed by the streaming loop to
// populate the agent.tool event (query + args) and delegation tracking.
type toolCallArgs struct {
	// Parsed is the full JSON args as a typed map for the mobile l10n layer.
	Parsed map[string]any
	// Query is the human-readable primary arg extracted via ToolQueryField.
	Query string
}

// persistedMsg carries the mutable state produced by persistUserMessage.
// It lives on streamSession rather than on SendMessageOpts so that opts
// remains read-only after construction.
// NOTE: onPersisted is a deprecated escape hatch — see the TODO in
// persistUserMessage for the planned refactor to return userMsg directly.
type persistedMsg struct {
	userMessageID string
	localID       string
	deliveredOnce *sync.Once
	readOnce      *sync.Once
	onPersisted   func(*orchestrator.Message)
}

// streamSession owns all mutable state for one callAgentStreaming invocation.
// Created at the start of the streaming call, methods on this struct replace
// the 7-8 parameter functions that previously threaded context through the pipeline.
type streamSession struct {
	svc          *Service
	sess         *dbr.Session
	wsID         string
	userID       string
	convID       string
	conversation *orchestrator.Conversation
	primary      *orchestrator.Agent
	lookups      agentLookups
	placeholder  *orchestrator.Message
	streams      map[string]*subAgentStream
	pending      map[string]pendingToolCall
	log          *slog.Logger

	// User message tracking (moved from SendMessageOpts mutation).
	userMessageID string
	localID       string
	deliveredOnce *sync.Once
	readOnce      *sync.Once
	onPersisted   func(*orchestrator.Message)

	// Stream metrics.
	startTime   time.Time
	totalChunks int
	globalDone  bool
	firstChunk  bool
}

// newStreamSessionOpts groups the inputs for newStreamSession.
type newStreamSessionOpts struct {
	svc         *Service
	sendOpts    *orchestratorservice.SendMessageOpts
	pm          *persistedMsg
	conv        *orchestrator.Conversation
	agent       *orchestrator.Agent
	lookups     agentLookups
	placeholder *orchestrator.Message
}

// callAgentStreamingOpts groups the inputs for callAgentStreaming.
type callAgentStreamingOpts struct {
	sendOpts     *orchestratorservice.SendMessageOpts
	pm           *persistedMsg
	conversation *orchestrator.Conversation
	runtimeState *orchestrator.RuntimeStatus
	agent        *orchestrator.Agent
	lookups      agentLookups
	extraContext string
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
