// Package mcp provides the MCP (Model Context Protocol) server for the Crawbl orchestrator.
//
// ZeroClaw agent runtimes connect to this server as MCP clients to access
// platform capabilities: push notifications, user context, and (future) OAuth
// integrations. The server is embedded in the orchestrator at /mcp/v1.
//
// Security model:
//   - Each ZeroClaw pod gets an HMAC-signed bearer token at provisioning time.
//   - The token encodes userID:workspaceID and is validated on every request.
//   - Tool handlers can only access data for the authenticated user.
//   - OAuth tokens for integrations are stored server-side; agents never see them.
package mcp

import (
	"context"
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/firebase"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
)

// contextKey is a private type for context value keys to avoid collisions.
type contextKey string

const (
	ctxKeyUserID      contextKey = "mcp_user_id"
	ctxKeyWorkspaceID contextKey = "mcp_workspace_id"
	ctxKeySessionID   contextKey = "mcp_session_id"
	ctxKeyAPICalls    contextKey = "mcp_api_calls"
)

// Deps holds all dependencies needed by the MCP server and tool handlers.
type Deps struct {
	DB               *dbr.Connection
	Logger           *slog.Logger
	UserRepo         orchestratorrepo.UserRepo
	WorkspaceRepo    orchestratorrepo.WorkspaceRepo
	ConversationRepo orchestratorrepo.ConversationRepo
	MessageRepo      orchestratorrepo.MessageRepo
	AgentRepo        orchestratorrepo.AgentRepo
	SigningKey        string
	FCM              *firebase.FCMClient // nil = push notifications disabled
}

// Config holds MCP server configuration from environment variables.
type Config struct {
	// SigningKey is the HMAC secret for generating/validating per-swarm tokens.
	SigningKey string
	// FCMProjectID is the Firebase project ID for push notifications.
	FCMProjectID string
	// FCMServiceAccountPath is the path to the Firebase service account JSON.
	FCMServiceAccountPath string
	// Endpoint is the full URL ZeroClaw pods use to reach this MCP server.
	// Example: http://orchestrator.backend.svc.cluster.local:7171/mcp/v1
	Endpoint string
}

func userIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyUserID).(string)
	return v
}

func workspaceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyWorkspaceID).(string)
	return v
}

func sessionIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeySessionID).(string)
	return v
}

func contextWithIdentity(ctx context.Context, userID, workspaceID, sessionID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyUserID, userID)
	ctx = context.WithValue(ctx, ctxKeyWorkspaceID, workspaceID)
	ctx = context.WithValue(ctx, ctxKeySessionID, sessionID)
	// Initialize API call tracker for outgoing call auditing.
	calls := make([]string, 0, 4)
	ctx = context.WithValue(ctx, ctxKeyAPICalls, &calls)
	return ctx
}

// newSession creates a new database session for MCP tool queries.
func (d *Deps) newSession() *dbr.Session {
	return d.DB.NewSession(nil)
}

// ---------------------------------------------------------------------------
// Tool input/output types — push notifications
// ---------------------------------------------------------------------------

type pushInput struct {
	Title   string `json:"title" jsonschema:"the notification title shown on the device"`
	Message string `json:"message" jsonschema:"the notification body text"`
}

type pushOutput struct {
	Sent bool   `json:"sent"`
	Info string `json:"info"`
}

// ---------------------------------------------------------------------------
// Tool input/output types — user context
// ---------------------------------------------------------------------------

// Note: empty input structs need at least one field to generate valid OpenAI tool schemas.
// OpenAI rejects {"type":"object","additionalProperties":false} without "properties".

type userProfileInput struct {
	IncludePreferences bool `json:"include_preferences,omitempty" jsonschema:"include user preferences in response"`
}

type userProfileOutput struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Nickname    string  `json:"nickname"`
	Name        string  `json:"name"`
	Surname     string  `json:"surname"`
	CountryCode *string `json:"country_code,omitempty"`
	CreatedAt   string  `json:"created_at"`
	Preferences *userPrefs  `json:"preferences,omitempty"`
}

type userPrefs struct {
	Theme    *string `json:"theme,omitempty"`
	Language *string `json:"language,omitempty"`
	Currency *string `json:"currency,omitempty"`
}

type workspaceInfoInput struct {
	IncludeAgents bool `json:"include_agents,omitempty" jsonschema:"include agent list in response"`
}

type workspaceInfoOutput struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	CreatedAt string       `json:"created_at"`
	Agents    []agentBrief `json:"agents"`
}

type agentBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
	Slug string `json:"slug"`
}

// ---------------------------------------------------------------------------
// Tool input/output types — conversations
// ---------------------------------------------------------------------------

type listConversationsInput struct {
	IncludeArchived bool `json:"include_archived,omitempty" jsonschema:"include archived conversations"`
}

type listConversationsOutput struct {
	Conversations []conversationBrief `json:"conversations"`
}

type conversationBrief struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Type      string  `json:"type"`
	AgentID   *string `json:"agent_id,omitempty"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type searchMessagesInput struct {
	ConversationID string `json:"conversation_id" jsonschema:"ID of the conversation to search in"`
	Query          string `json:"query" jsonschema:"search keyword or phrase"`
	Limit          int    `json:"limit" jsonschema:"maximum results to return (default 20, max 50)"`
}

type searchMessagesOutput struct {
	Messages []messageBrief `json:"messages"`
	Count    int            `json:"count"`
}

type messageBrief struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

// messageRow is a database row type for message search queries.
type messageRow struct {
	ID        string    `db:"id"`
	Role      string    `db:"role"`
	Content   string    `db:"content"`
	CreatedAt time.Time `db:"created_at"`
}

// ---------------------------------------------------------------------------
// Audit log types
// ---------------------------------------------------------------------------

// auditEntry holds the fields for a single MCP tool call audit record.
type auditEntry struct {
	UserID      string
	WorkspaceID string
	SessionID   string
	ToolName    string
	Input       string
	Output      string // JSON response returned to the agent
	APICalls    string // outgoing API calls made by this tool (e.g. "FCM:POST /v1/projects/crawbl-dev/messages:send")
	Success     bool
	ErrorMsg    string
	DurationMs  int
}
