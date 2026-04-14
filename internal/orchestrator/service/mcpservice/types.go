// Package mcpservice implements the business logic for MCP tool operations.
// It sits between the MCP HTTP handlers and the persistence layer, providing
// user profile, push notification, chat, agent messaging, artifact,
// and workflow operations.
package mcpservice

import (
	"context"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/artifactrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/mcprepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
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
	SearchMessages(ctx context.Context, sess *dbr.Session, userID, workspaceID, conversationID, query string, limit int) ([]MessageBrief, error)

	// Push notifications
	SendPush(ctx context.Context, sess *dbr.Session, userID, title, message string) (sent bool, info string, err error)

	// Agent operations
	ResolveAgentBySlug(ctx context.Context, sess *dbr.Session, workspaceID, slug string) (string, error)
	CreateAgentHistory(ctx context.Context, sess *dbr.Session, workspaceID string, params *CreateAgentHistoryParams) error

	// Agent-to-agent messaging
	SendMessageToAgent(ctx context.Context, sess *dbr.Session, params *SendAgentMessageParams) (*SendAgentMessageResult, error)

	// Artifacts
	CreateArtifact(ctx context.Context, sess *dbr.Session, userID, workspaceID string, params *CreateArtifactParams) (*CreateArtifactResult, error)
	ReadArtifact(ctx context.Context, sess *dbr.Session, userID, workspaceID, artifactID string, version int) (*ReadArtifactResult, error)
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
	Workspace    workspaceStore
	Conversation conversationStore
	Agent        agentStore
	AgentHistory agentHistoryStore
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
	// ShutdownCtx is the server-lifetime context. Goroutines spawned by the
	// service (e.g. workflow executors) derive their contexts from this so
	// they die on SIGTERM rather than surviving the HTTP request or leaking.
	// If nil, workflow goroutines fall back to context.Background() (useful
	// for tests).
	ShutdownCtx context.Context
}

type logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

// UserProfileResult is returned by GetUserProfile.
type UserProfileResult struct {
	ID          string
	Email       string
	Nickname    string
	Name        string
	Surname     string
	CountryCode *string
	CreatedAt   time.Time
	Preferences *UserPreferences
}

// UserPreferences holds optional user preference fields.
type UserPreferences struct {
	Theme    *string
	Language *string
	Currency *string
}

// WorkspaceInfoResult is returned by GetWorkspaceInfo.
type WorkspaceInfoResult struct {
	ID        string
	Name      string
	CreatedAt time.Time
	Agents    []*orchestrator.Agent
}

// MessageBrief is a search result item.
type MessageBrief struct {
	ID        string
	Role      string
	Text      string
	CreatedAt time.Time
}

// CreateAgentHistoryParams holds input for creating an agent history entry.
type CreateAgentHistoryParams struct {
	AgentID        string
	AgentSlug      string
	ConversationID string
	Title          string
	Subtitle       string
}

// SendAgentMessageParams holds input for the send_message_to_agent flow.
type SendAgentMessageParams struct {
	UserID         string
	WorkspaceID    string
	SessionID      string
	AgentSlug      string
	Message        string
	ConversationID string
}

// SendAgentMessageResult is returned by SendMessageToAgent.
type SendAgentMessageResult struct {
	Success   bool
	AgentSlug string
	Response  string
	MessageID string
	Error     string
}

// CreateArtifactParams holds input for creating an artifact.
type CreateArtifactParams struct {
	Title          string
	Content        string
	ContentType    string
	ConversationID string
	AgentID        string
	AgentSlug      string
}

// CreateArtifactResult is returned by CreateArtifact.
type CreateArtifactResult struct {
	ArtifactID string
	Version    int
}

// ReadArtifactResult is returned by ReadArtifact.
type ReadArtifactResult struct {
	ArtifactID  string
	Title       string
	ContentType string
	Content     string
	Version     int
	Status      string
	Reviews     []ArtifactReviewBrief
}

// ArtifactReviewBrief summarises a single review.
type ArtifactReviewBrief struct {
	ReviewerAgentSlug string
	Outcome           string
	Comments          string
	CreatedAt         time.Time
}

// UpdateArtifactParams holds input for updating an artifact.
type UpdateArtifactParams struct {
	ArtifactID      string
	Content         string
	ChangeSummary   string
	ExpectedVersion int
	AgentID         string
	AgentSlug       string
}

// UpdateArtifactResult is returned by UpdateArtifact.
type UpdateArtifactResult struct {
	Version int
}

// ReviewArtifactParams holds input for reviewing an artifact.
type ReviewArtifactParams struct {
	ArtifactID string
	Outcome    string
	Comments   string
	Version    int
	AgentID    string
	AgentSlug  string
}

// ReviewArtifactResult is returned by ReviewArtifact.
type ReviewArtifactResult struct {
	Reviewed bool
}

// AskQuestionsParams holds input for creating an interactive questions message.
type AskQuestionsParams struct {
	AgentID        string
	AgentSlug      string
	ConversationID string
	Turns          []AskQuestionsTurn
}

// AskQuestionsTurn describes one turn group to present to the user.
type AskQuestionsTurn struct {
	Label     string
	Questions []AskQuestionsQuestion
}

// AskQuestionsQuestion describes one question with its allowed options.
type AskQuestionsQuestion struct {
	Prompt  string
	Mode    string   // "single" or "multi"
	Options []string // option labels; service assigns A, B, C, …
}

// AskQuestionsResult is returned by AskQuestions.
type AskQuestionsResult struct {
	MessageID string
}

// CreateWorkflowParams holds input for creating a workflow definition.
type CreateWorkflowParams struct {
	Name        string
	Description string
	StepsJSON   string
}

// CreateWorkflowResult is returned by CreateWorkflow.
type CreateWorkflowResult struct {
	WorkflowID string
	StepCount  int
}

// TriggerWorkflowParams holds input for triggering a workflow.
type TriggerWorkflowParams struct {
	WorkflowID     string
	ConversationID string
	InitialContext string
}

// TriggerWorkflowResult is returned by TriggerWorkflow.
type TriggerWorkflowResult struct {
	ExecutionID  string
	WorkflowName string
}

// WorkflowStatusResult is returned by CheckWorkflowStatus.
type WorkflowStatusResult struct {
	ExecutionID string
	Status      string
	CurrentStep int
	Error       string
	Steps       []StepStatusBrief
}

// StepStatusBrief summarises a single workflow step execution.
type StepStatusBrief struct {
	StepIndex  int
	StepName   string
	AgentSlug  string
	Status     string
	DurationMs *int
}

// WorkflowBriefResult summarises a workflow definition.
type WorkflowBriefResult struct {
	ID          string
	Name        string
	Description string
	IsActive    bool
	StepCount   int
	CreatedAt   time.Time
}

// WorkflowStep describes a single step within a workflow definition.
type WorkflowStep struct {
	Name             string `json:"name"`
	AgentSlug        string `json:"agent_slug"`
	PromptTemplate   string `json:"prompt_template"`
	TimeoutSecs      int    `json:"timeout_secs,omitempty"`
	RequiresApproval bool   `json:"requires_approval,omitempty"`
	OnFailure        string `json:"on_failure,omitempty"`
	OutputKey        string `json:"output_key,omitempty"`
	MaxRetries       int    `json:"max_retries,omitempty"`
}
