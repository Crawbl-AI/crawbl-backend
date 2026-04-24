// Package handler provides HTTP handler functions for the orchestrator API.
// Each handler is a function that takes a *Context and returns an http.HandlerFunc,
// enabling dependency injection without receiver methods.
package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gocraft/dbr/v2"
	"google.golang.org/protobuf/encoding/protojson"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/realtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/middleware"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// Const declarations

const (
	// errInvalidRequestBody is the error message returned when a request body cannot be decoded.
	errInvalidRequestBody = "invalid request body"

	// Agent memory field length limits — mirror the MCP path enforcement.
	MaxAgentMemoryKeyLength      = 256
	MaxAgentMemoryContentLength  = 16 * 1024 // 16 KiB
	MaxAgentMemoryCategoryLength = 128

	// maxProtoBodySize is the upper bound for request bodies decoded by
	// DecodeProtoJSON. Requests larger than 1 MiB are silently truncated,
	// which causes an unmarshal error — preventing OOM from oversized payloads.
	maxProtoBodySize = 1 << 20 // 1 MiB
)

// Var declarations

// protoMarshaler is the shared protojson marshal options for all proto
// responses. UseProtoNames emits snake_case field names matching the
// proto definitions and current JSON wire format.
var protoMarshaler = protojson.MarshalOptions{
	UseProtoNames:   true,
	EmitUnpopulated: true,
}

// protoUnmarshaler is the shared protojson unmarshal options for all
// proto request bodies. DiscardUnknown allows forward-compatible clients.
var protoUnmarshaler = protojson.UnmarshalOptions{
	DiscardUnknown: true,
}

// Type declarations

// Context holds shared dependencies for all handlers.
//
// Service fields use the consumer-side interfaces declared in ports.go
// (AuthPort / WorkspacePort / ChatPort / AgentPort / IntegrationPort)
// so handlers never import the producer-owned service contracts.
type Context struct {
	// DB is the database connection pool for all persistence operations.
	DB *dbr.Connection

	// Logger provides structured logging throughout the handler lifecycle.
	Logger *slog.Logger

	// AuthService handles user authentication, registration, and profile management.
	AuthService AuthPort

	// WorkspaceService manages workspace provisioning and runtime state.
	WorkspaceService WorkspacePort

	// ChatService handles conversations, messages, and agent interactions.
	ChatService ChatPort

	// AgentService handles agent details, settings, tools, and history retrieval.
	AgentService AgentPort

	// IntegrationService manages third-party OAuth connections.
	IntegrationService IntegrationPort

	// HTTPMiddleware contains authentication and request middleware configuration.
	HTTPMiddleware *middleware.MiddlewareConfig

	// Broadcaster emits real-time events to connected WebSocket clients.
	Broadcaster realtime.Broadcaster

	// RuntimeClient manages agent runtime CRs for workspace provisioning and cleanup.
	RuntimeClient userswarmclient.Client

	// MCPSigningKey is the HMAC signing key for internal MCP/runtime bearer tokens.
	MCPSigningKey string

	// UsageRepo provides token usage and quota read operations for usage API endpoints.
	UsageRepo usagerepo.Repo
}

// AuthedHandlerDeps bundles the per-request dependencies that every authed
// handler needs: the handler context and the authenticated user. Handlers
// receive this struct so they have everything required to talk to services
// without re-plumbing boilerplate. The database session is carried on the
// request context via the session middleware.
type AuthedHandlerDeps struct {
	// Ctx is the shared handler context (services, logger, broadcaster, ...).
	Ctx *Context
	// User is the authenticated, non-banned, non-deleted caller.
	User *orchestrator.User
}

// AuthedJSONFunc is the business logic signature for handlers that read a
// JSON request body. The decorator decodes the body into Req before calling.
// The handler returns the response payload (wrapped in the success envelope
// automatically) and an optional domain error.
type AuthedJSONFunc[Req any, Resp any] func(
	r *http.Request,
	deps *AuthedHandlerDeps,
	req *Req,
) (Resp, *merrors.Error)

// AuthedFunc is the business logic signature for handlers that do not read a
// request body (GET, DELETE, or handlers that pull all inputs from URL/query
// params). The decorator skips body decoding entirely.
type AuthedFunc[Resp any] func(
	r *http.Request,
	deps *AuthedHandlerDeps,
) (Resp, *merrors.Error)

// internalWorkspaceBlueprint is the wire shape returned by
// GET /v1/internal/agents. Field names MUST match
// internal/agentruntime/runner/blueprint.go WorkspaceBlueprint so the
// runtime decodes the response directly. Keeping the type private to
// this package (no export) because the only valid consumer is the
// runtime's blueprint client.
type internalWorkspaceBlueprint struct {
	WorkspaceID string                   `json:"workspace_id"`
	Agents      []internalAgentBlueprint `json:"agents"`
}

// internalAgentBlueprint is the per-agent wire shape within internalWorkspaceBlueprint.
type internalAgentBlueprint struct {
	Slug         string   `json:"slug"`
	Role         string   `json:"role"`
	SystemPrompt string   `json:"system_prompt"`
	Description  string   `json:"description"`
	AllowedTools []string `json:"allowed_tools"`
	Model        string   `json:"model"`
}

// Port interface declarations

// AuthPort is the subset of the authentication service the HTTP
// handlers actually call into.
type AuthPort interface {
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

// WorkspacePort is the subset of the workspace service the handlers
// depend on.
type WorkspacePort interface {
	ListByUserID(ctx context.Context, opts *orchestratorservice.ListWorkspacesOpts) ([]*orchestrator.Workspace, *merrors.Error)
	GetByID(ctx context.Context, opts *orchestratorservice.GetWorkspaceOpts) (*orchestrator.Workspace, *merrors.Error)
}

// ChatPort is the subset of the chat service the HTTP handlers depend
// on: conversation/message/agent lookups + send/reply and workspace
// summary rendering.
type ChatPort interface {
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

// AgentPort is the subset of the agent service the handlers depend on.
type AgentPort interface {
	GetAgent(ctx context.Context, opts *orchestratorservice.GetAgentOpts) (*orchestrator.Agent, *merrors.Error)
	GetAgentDetails(ctx context.Context, opts *orchestratorservice.GetAgentDetailsOpts) (*orchestrator.AgentDetails, *merrors.Error)
	GetAgentHistory(ctx context.Context, opts *orchestratorservice.GetAgentHistoryOpts) ([]orchestrator.AgentHistoryItem, *orchestrator.OffsetPagination, *merrors.Error)
	GetAgentSettings(ctx context.Context, opts *orchestratorservice.GetAgentSettingsOpts) (*orchestrator.AgentSettings, *merrors.Error)
	GetAgentTools(ctx context.Context, opts *orchestratorservice.GetAgentToolsOpts) (*orchestrator.ToolPage, *merrors.Error)
	GetAgentMemories(ctx context.Context, opts *orchestratorservice.GetAgentMemoriesOpts) ([]orchestratorservice.AgentMemory, *merrors.Error)
	DeleteAgentMemory(ctx context.Context, opts *orchestratorservice.DeleteAgentMemoryOpts) *merrors.Error
	CreateAgentMemory(ctx context.Context, opts *orchestratorservice.CreateAgentMemoryOpts) (*orchestratorservice.AgentMemory, *merrors.Error)
}

// IntegrationPort is the subset of the integration service the handlers
// depend on.
type IntegrationPort interface {
	ListIntegrations(ctx context.Context, opts *orchestratorservice.ListIntegrationsOpts) ([]*orchestrator.IntegrationItem, *merrors.Error)
	GetOAuthConfig(ctx context.Context, opts *orchestratorservice.GetOAuthConfigOpts) (*orchestrator.OAuthConfig, *merrors.Error)
	HandleOAuthCallback(ctx context.Context, opts *orchestratorservice.OAuthCallbackOpts) *merrors.Error
}
