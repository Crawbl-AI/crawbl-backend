// Package handler — ports.go declares the narrow service contracts the
// HTTP handlers depend on. Per project convention, interfaces live at
// the consumer, not the producer: each method listed here corresponds
// to a call site in a handler file. The concrete services exported
// from internal/orchestrator/service/... satisfy these implicitly via
// Go's structural typing.
package handler

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// authPort is the subset of the authentication service the HTTP
// handlers actually call into.
type authPort interface {
	SignUp(ctx context.Context, opts *orchestratorservice.SignUpOpts) (*orchestrator.User, *merrors.Error)
	SignIn(ctx context.Context, opts *orchestratorservice.SignInOpts) (*orchestrator.User, *merrors.Error)
	Delete(ctx context.Context, opts *orchestratorservice.DeleteOpts) *merrors.Error
	GetBySubject(ctx context.Context, opts *orchestratorservice.GetUserBySubjectOpts) (*orchestrator.User, *merrors.Error)
	UpdateProfile(ctx context.Context, opts *orchestratorservice.UpdateProfileOpts) (*orchestrator.User, *merrors.Error)
	GetLegalDocuments(ctx context.Context) (*orchestrator.LegalDocuments, *merrors.Error)
	AcceptLegal(ctx context.Context, opts *orchestratorservice.AcceptLegalOpts) (*orchestrator.User, *merrors.Error)
	SavePushToken(ctx context.Context, opts *orchestratorservice.SavePushTokenOpts) *merrors.Error
	ClearPushToken(ctx context.Context, opts *orchestratorservice.ClearPushTokenOpts) *merrors.Error
}

// workspacePort is the subset of the workspace service the handlers
// depend on.
type workspacePort interface {
	ListByUserID(ctx context.Context, opts *orchestratorservice.ListWorkspacesOpts) ([]*orchestrator.Workspace, *merrors.Error)
	GetByID(ctx context.Context, opts *orchestratorservice.GetWorkspaceOpts) (*orchestrator.Workspace, *merrors.Error)
}

// chatPort is the subset of the chat service the HTTP handlers depend
// on: conversation/message/agent lookups + send/reply and workspace
// summary rendering.
type chatPort interface {
	ListAgents(ctx context.Context, opts *orchestratorservice.ListAgentsOpts) ([]*orchestrator.Agent, *merrors.Error)
	ListConversations(ctx context.Context, opts *orchestratorservice.ListConversationsOpts) ([]*orchestrator.Conversation, *merrors.Error)
	GetConversation(ctx context.Context, opts *orchestratorservice.GetConversationOpts) (*orchestrator.Conversation, *merrors.Error)
	ListMessages(ctx context.Context, opts *orchestratorservice.ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error)
	GetWorkspaceSummary(ctx context.Context, opts *orchestratorservice.GetWorkspaceSummaryOpts) (*orchestrator.WorkspaceSummary, *merrors.Error)
	SendMessage(ctx context.Context, opts *orchestratorservice.SendMessageOpts) ([]*orchestrator.Message, *merrors.Error)
	CreateConversation(ctx context.Context, opts *orchestratorservice.CreateConversationOpts) (*orchestrator.Conversation, *merrors.Error)
	DeleteConversation(ctx context.Context, opts *orchestratorservice.DeleteConversationOpts) *merrors.Error
	MarkConversationRead(ctx context.Context, opts *orchestratorservice.MarkConversationReadOpts) *merrors.Error
	RespondToActionCard(ctx context.Context, opts *orchestratorservice.RespondToActionCardOpts) (*orchestrator.Message, *merrors.Error)
}

// agentPort is the subset of the agent service the handlers depend on.
type agentPort interface {
	GetAgent(ctx context.Context, opts *orchestratorservice.GetAgentOpts) (*orchestrator.Agent, *merrors.Error)
	GetAgentDetails(ctx context.Context, opts *orchestratorservice.GetAgentDetailsOpts) (*orchestrator.AgentDetails, *merrors.Error)
	GetAgentHistory(ctx context.Context, opts *orchestratorservice.GetAgentHistoryOpts) ([]orchestrator.AgentHistoryItem, *orchestrator.OffsetPagination, *merrors.Error)
	GetAgentSettings(ctx context.Context, opts *orchestratorservice.GetAgentSettingsOpts) (*orchestrator.AgentSettings, *merrors.Error)
	GetAgentTools(ctx context.Context, opts *orchestratorservice.GetAgentToolsOpts) (*orchestrator.ToolPage, *merrors.Error)
	GetAgentMemories(ctx context.Context, opts *orchestratorservice.GetAgentMemoriesOpts) ([]orchestratorservice.AgentMemory, *merrors.Error)
	DeleteAgentMemory(ctx context.Context, opts *orchestratorservice.DeleteAgentMemoryOpts) *merrors.Error
	CreateAgentMemory(ctx context.Context, opts *orchestratorservice.CreateAgentMemoryOpts) *merrors.Error
}

// integrationPort is the subset of the integration service the handlers
// depend on.
type integrationPort interface {
	ListIntegrations(ctx context.Context, opts *orchestratorservice.ListIntegrationsOpts) ([]*orchestrator.IntegrationItem, *merrors.Error)
	GetOAuthConfig(ctx context.Context, opts *orchestratorservice.GetOAuthConfigOpts) (*orchestrator.OAuthConfig, *merrors.Error)
	HandleOAuthCallback(ctx context.Context, opts *orchestratorservice.OAuthCallbackOpts) *merrors.Error
}
