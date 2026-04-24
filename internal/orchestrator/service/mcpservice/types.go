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
	mcpv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mcp/v1"
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
	UserPreferences        = mcpv1.UserPreferences
	UserProfileResult      = mcpv1.UserProfileResult
	MessageBrief           = mcpv1.MessageBrief
	CreateAgentHistoryParams = mcpv1.CreateAgentHistoryParams
	SendAgentMessageParams = mcpv1.SendAgentMessageParams
	SendAgentMessageResult = mcpv1.SendAgentMessageResult
	CreateArtifactParams   = mcpv1.CreateArtifactParams
	CreateArtifactResult   = mcpv1.CreateArtifactResult
	ReadArtifactResult     = mcpv1.ReadArtifactResult
	ArtifactReviewBrief    = mcpv1.ArtifactReviewBrief
	UpdateArtifactParams   = mcpv1.UpdateArtifactParams
	UpdateArtifactResult   = mcpv1.UpdateArtifactResult
	ReviewArtifactParams   = mcpv1.ReviewArtifactParams
	ReviewArtifactResult   = mcpv1.ReviewArtifactResult
	AskQuestionsParams     = mcpv1.AskQuestionsParams
	AskQuestionsTurn       = mcpv1.AskQuestionsTurn
	AskQuestionsQuestion   = mcpv1.AskQuestionsQuestion
	AskQuestionsResult     = mcpv1.AskQuestionsResult
	CreateWorkflowParams   = mcpv1.CreateWorkflowParams
	CreateWorkflowResult   = mcpv1.CreateWorkflowResult
	TriggerWorkflowParams  = mcpv1.TriggerWorkflowParams
	TriggerWorkflowResult  = mcpv1.TriggerWorkflowResult
	WorkflowStatusResult   = mcpv1.WorkflowStatusResult
	StepStatusBrief        = mcpv1.StepStatusBrief
	WorkflowBriefResult    = mcpv1.WorkflowBriefResult
	WorkflowStep           = mcpv1.WorkflowStep
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
