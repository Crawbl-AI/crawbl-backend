// Package server — ports.go declares the narrow service contracts the
// orchestrator HTTP server holds. Per project convention, interfaces
// live at the consumer, not the producer. Because the server owns the
// handler.Context construction path, each of these is a structural
// superset of what the handlers need: the same concrete service
// implementation satisfies both the server-owned interface here and
// the handler-owned interface in handler/ports.go.
package server

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// authPort mirrors the handler-side authPort and is used here so the
// server can store service handles without importing the producer
// AuthService interface directly.
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

// workspacePort mirrors handler-side workspacePort.
type workspacePort interface {
	ListByUserID(ctx context.Context, opts *orchestratorservice.ListWorkspacesOpts) ([]*orchestrator.Workspace, *merrors.Error)
	GetByID(ctx context.Context, opts *orchestratorservice.GetWorkspaceOpts) (*orchestrator.Workspace, *merrors.Error)
}

// chatPort mirrors handler-side chatPort.
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

// agentPort mirrors handler-side agentPort.
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

// integrationPort mirrors handler-side integrationPort.
type integrationPort interface {
	ListIntegrations(ctx context.Context, opts *orchestratorservice.ListIntegrationsOpts) ([]*orchestrator.IntegrationItem, *merrors.Error)
	GetOAuthConfig(ctx context.Context, opts *orchestratorservice.GetOAuthConfigOpts) (*orchestrator.OAuthConfig, *merrors.Error)
	HandleOAuthCallback(ctx context.Context, opts *orchestratorservice.OAuthCallbackOpts) *merrors.Error
}
