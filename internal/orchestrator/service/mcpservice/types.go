// Package mcpservice implements the business logic for MCP tool operations.
// It sits between the MCP HTTP handlers and the persistence layer, providing
// user profile, push notification, chat, agent messaging, artifact,
// and workflow operations.
package mcpservice

import (
	"context"
	"errors"
	"time"

	"github.com/gocraft/dbr/v2"

	mcpv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mcp/v1"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/artifactrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/mcprepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/firebase"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// Service defines all operations that MCP tool handlers need.
type Service interface {
	// User context
	GetUserProfile(ctx context.Context, sess *dbr.Session, userID string, includePrefs bool) (*UserProfileResult, error)
	GetWorkspaceInfo(ctx context.Context, sess *dbr.Session, userID, workspaceID string, includeAgents bool) (*WorkspaceInfoResult, error)

	// Conversations & messages
	ListConversations(ctx context.Context, sess *dbr.Session, userID, workspaceID string) ([]*orchestrator.Conversation, error)
	SearchMessages(ctx context.Context, sess *dbr.Session, opts SearchMessagesOpts) ([]MessageBrief, error)

	// Push notifications
	SendPush(ctx context.Context, sess *dbr.Session, userID, title, message string) (sent bool, info string, err error)

	// Agent operations
	ResolveAgentBySlug(ctx context.Context, sess *dbr.Session, workspaceID, slug string) (string, error)
	CreateAgentHistory(ctx context.Context, sess *dbr.Session, workspaceID string, params *CreateAgentHistoryParams) error

	// Agent-to-agent messaging
	SendMessageToAgent(ctx context.Context, sess *dbr.Session, params *SendAgentMessageParams) (*SendAgentMessageResult, error)

	// Artifacts
	CreateArtifact(ctx context.Context, sess *dbr.Session, userID, workspaceID string, params *CreateArtifactParams) (*CreateArtifactResult, error)
	ReadArtifact(ctx context.Context, sess *dbr.Session, opts ReadArtifactOpts) (*ReadArtifactResult, error)
	UpdateArtifact(ctx context.Context, sess *dbr.Session, userID, workspaceID string, params *UpdateArtifactParams) (*UpdateArtifactResult, error)
	ReviewArtifact(ctx context.Context, sess *dbr.Session, userID, workspaceID string, params *ReviewArtifactParams) (*ReviewArtifactResult, error)

	// Questions
	// AskQuestions creates an interactive questions message for the given conversation,
	// persists it with role=agent and content.type="questions", and broadcasts it.
	AskQuestions(ctx context.Context, sess *dbr.Session, userID, workspaceID string, params *AskQuestionsParams) (*AskQuestionsResult, error)

	// Workflows
	CreateWorkflow(ctx context.Context, sess *dbr.Session, workspaceID string, params *CreateWorkflowParams) (*CreateWorkflowResult, error)
	TriggerWorkflow(ctx context.Context, sess *dbr.Session, userID, workspaceID string, params *TriggerWorkflowParams) (*TriggerWorkflowResult, error)
	CheckWorkflowStatus(ctx context.Context, sess *dbr.Session, workspaceID, executionID string) (*WorkflowStatusResult, error)
	ListWorkflows(ctx context.Context, sess *dbr.Session, userID, workspaceID string) ([]WorkflowBriefResult, error)
}

// WorkflowExecutor runs a workflow execution asynchronously.
type WorkflowExecutor interface {
	ExecuteWorkflow(ctx context.Context, executionID, workspaceID string, runtime *orchestrator.RuntimeStatus)
}

// Repos groups the repository dependencies. Fields are typed against
// consumer-side interfaces declared in ports.go so the package does not
// import producer-owned interfaces for its own internal plumbing.
type Repos struct {
	MCP          mcprepo.Repo
	Workspace    workspaceGetter
	Conversation conversationStore
	Agent        agentStore
	AgentHistory agentHistoryCreator
	Message      messageStore
	Artifact     artifactrepo.Repo
	Workflow     workflowrepo.Repo
}

// Infra groups non-repo infrastructure dependencies.
type Infra struct {
	Logger        logger
	FCM           *firebase.FCMClient
	RuntimeClient userswarmclient.Client
	Broadcaster   realtime.Broadcaster
	WorkflowExec  WorkflowExecutor
}

type logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

// Proto-generated type aliases. The canonical definitions live in
// internal/generated/proto/mcp/v1/mcp.pb.go; these aliases keep
// existing callers compiling without a package-wide import change.
type (
	UserPreferences          = mcpv1.UserPreferences
	UserProfileResult        = mcpv1.UserProfileResult
	MessageBrief             = mcpv1.MessageBrief
	CreateAgentHistoryParams = mcpv1.CreateAgentHistoryParams
	SendAgentMessageParams   = mcpv1.SendAgentMessageParams
	SendAgentMessageResult   = mcpv1.SendAgentMessageResult
	CreateArtifactParams     = mcpv1.CreateArtifactParams
	CreateArtifactResult     = mcpv1.CreateArtifactResult
	ReadArtifactResult       = mcpv1.ReadArtifactResult
	ArtifactReviewBrief      = mcpv1.ArtifactReviewBrief
	UpdateArtifactParams     = mcpv1.UpdateArtifactParams
	UpdateArtifactResult     = mcpv1.UpdateArtifactResult
	ReviewArtifactParams     = mcpv1.ReviewArtifactParams
	ReviewArtifactResult     = mcpv1.ReviewArtifactResult
	AskQuestionsParams       = mcpv1.AskQuestionsParams
	AskQuestionsTurn         = mcpv1.AskQuestionsTurn
	AskQuestionsQuestion     = mcpv1.AskQuestionsQuestion
	AskQuestionsResult       = mcpv1.AskQuestionsResult
	CreateWorkflowParams     = mcpv1.CreateWorkflowParams
	CreateWorkflowResult     = mcpv1.CreateWorkflowResult
	TriggerWorkflowParams    = mcpv1.TriggerWorkflowParams
	TriggerWorkflowResult    = mcpv1.TriggerWorkflowResult
	WorkflowStatusResult     = mcpv1.WorkflowStatusResult
	StepStatusBrief          = mcpv1.StepStatusBrief
	WorkflowBriefResult      = mcpv1.WorkflowBriefResult
	WorkflowStep             = mcpv1.WorkflowStep
)

// WorkspaceInfoResult is returned by GetWorkspaceInfo.
// Kept as a Go struct because Agents uses []*orchestrator.Agent which
// does not match the proto AgentBrief type.
type WorkspaceInfoResult struct {
	ID        string
	Name      string
	CreatedAt time.Time
	Agents    []*orchestrator.Agent
}

// Convenience type aliases to keep method signatures short.
type (
	contextT = context.Context
	sessionT = *dbr.Session
)

// Sentinel errors returned by the service.
var errWorkspaceNotFound = errors.New("workspace not found")

// service implements the Service interface.
type service struct {
	repos       Repos
	infra       Infra
	memoryStack layers.Stack
	// spawnWorkflow launches a workflow execution in a background goroutine
	// tied to the server-lifetime context. It captures shutdownCtx via closure
	// so the service struct does not store a context.Context field.
	spawnWorkflow func(executionID, workspaceID string, runtime *orchestrator.RuntimeStatus)
}

// workspaceGetter is the workspace subset mcpservice uses: ownership
// checks before returning workspace-scoped data to MCP tool callers.
type workspaceGetter interface {
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
}

// conversationStore is the conversation subset mcpservice uses: listing
// and single-conversation lookups for the search-messages tool.
type conversationStore interface {
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Conversation, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) (*orchestrator.Conversation, *merrors.Error)
}

// agentStore is the agent subset mcpservice uses: global lookup for
// sender-name enrichment plus per-workspace listing for agent rosters.
type agentStore interface {
	GetByIDGlobal(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestrator.Agent, *merrors.Error)
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error)
}

// agentHistoryCreator is the agent_history subset mcpservice uses to
// append history entries from MCP agent-creation tools.
type agentHistoryCreator interface {
	Create(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentHistoryRow) *merrors.Error
}

// messageStore is the message subset mcpservice uses: recent-message
// listing for the conversation-context tool and message persistence for
// agent-authored structured content (e.g. questions cards).
type messageStore interface {
	ListRecent(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string, limit int) ([]*orchestrator.Message, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, message *orchestrator.Message) *merrors.Error
}

// SearchMessagesOpts groups the parameters for SearchMessages so the call site
// reads as a labelled struct literal and the function signature stays under the
// project's 4-5 param limit.
type SearchMessagesOpts struct {
	UserID         string
	WorkspaceID    string
	ConversationID string
	Query          string
	Limit          int
}

// ReadArtifactOpts groups the parameters for ReadArtifact.
type ReadArtifactOpts struct {
	UserID      string
	WorkspaceID string
	ArtifactID  string
	Version     int
}

// emitDelegationOpts groups the parameters for emitDelegationStarted and
// emitDelegationDone so their signatures stay under the 4-5 param limit.
type emitDelegationOpts struct {
	WorkspaceID    string
	From           *orchestrator.Agent
	To             *orchestrator.Agent
	ConversationID string
	MsgID          string
	// Message is only used by emitDelegationStarted (the preview text).
	Message string
	// Status is only used by emitDelegationDone.
	Status string
}

// maxAgentDepth is the maximum depth for agent-to-agent chains.
const maxAgentDepth = 3

// delegationPreviewMaxRunes caps the preview text on delegation socket events.
const delegationPreviewMaxRunes = 100

// agentMessageMaxStoredBytes caps the response_text column on agent_messages.
const agentMessageMaxStoredBytes = 32768

// agentMessageTruncatedMarker is appended when a response exceeds the column budget.
const agentMessageTruncatedMarker = "\n[truncated]"

// contextMessageLimit is the number of recent messages to include as context.
const contextMessageLimit = 20

// prepareOpts groups the inputs for prepareAgentMessage.
type prepareOpts struct {
	params      *SendAgentMessageParams
	slug        string
	fromAgent   *orchestrator.Agent
	targetAgent *orchestrator.Agent
	callingSlug string
	message     string
}

// callRuntimeOpts groups the inputs for callAgentRuntime.
type callRuntimeOpts struct {
	params      *SendAgentMessageParams
	slug        string
	fromAgent   *orchestrator.Agent
	targetAgent *orchestrator.Agent
	callingSlug string
	message     string
	msgID       string
}

// failAgentMessageOpts groups the inputs for failAgentMessage.
type failAgentMessageOpts struct {
	ctx            contextT
	sess           sessionT
	msgID          string
	errText        string
	durationMs     int64
	workspaceID    string
	from           *orchestrator.Agent
	to             *orchestrator.Agent
	conversationID string
}

// agentContextMaxTextLen caps the text length per message when building conversation context.
const agentContextMaxTextLen = 500

// repoNamer is a layers.AgentNamer that performs a live DB lookup via the
// agent repo. Used by mcpservice where a pre-built agent map is not available.
type repoNamer struct {
	repo agentStore
}

// errArtifactNotFound is returned when an artifact lookup fails.
const errArtifactNotFound = "artifact not found"

// persistArtifactMessageOpts groups every field persistArtifactMessage
// needs. Grouping keeps the function signature under the project's
// 4-5 param limit (and SonarQube's go:S107 limit of 7) and makes the
// call sites read as a labelled struct literal instead of a long
// positional argument list where a string mix-up is silent.
type persistArtifactMessageOpts struct {
	WorkspaceID string
	AgentID     string
	ConvID      *string
	ArtifactID  string
	Title       string
	ContentType string
	Action      artifactrepo.ArtifactAction
	Version     int
	Status      string
}

// Option-ID generation uses sequential uppercase ASCII letters (A..Z). The
// cap on options per question is therefore fixed at the size of that range.
const (
	optionIDBase          = 'A'
	maxOptionsPerQuestion = 'Z' - optionIDBase + 1 // 26 — one letter per option
	minOptionsPerQuestion = 2
)

// Input caps for the ask_questions MCP tool. Kept as named constants so the
// limits are discoverable and easy to tune without touching validation logic.
const (
	maxTurnsPerMessage  = 10
	maxQuestionsPerTurn = 20
	maxPromptLen        = 2048
	maxOptionLabelLen   = 256
	maxTurnLabelLen     = 128
)

const errCodeInvalidInput = "invalid_input"
