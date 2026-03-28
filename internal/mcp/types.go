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
	"net/http"

	"github.com/gocraft/dbr/v2"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
)

// contextKey is a private type for context value keys to avoid collisions.
type contextKey string

const (
	ctxKeyUserID      contextKey = "mcp_user_id"
	ctxKeyWorkspaceID contextKey = "mcp_workspace_id"
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
	FCM              *FCMClient // nil = push notifications disabled
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

// FCMClient sends push notifications via Firebase Cloud Messaging v1 API.
type FCMClient struct {
	projectID  string
	httpClient *http.Client
	// tokenSource provides OAuth2 access tokens for the FCM API.
	// Stored as an interface to avoid importing oauth2 in this file.
	getAccessToken func(ctx context.Context) (string, error)
}

func userIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyUserID).(string)
	return v
}

func workspaceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyWorkspaceID).(string)
	return v
}

func contextWithIdentity(ctx context.Context, userID, workspaceID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyUserID, userID)
	ctx = context.WithValue(ctx, ctxKeyWorkspaceID, workspaceID)
	return ctx
}

// newSession creates a new database session for MCP tool queries.
func (d *Deps) newSession() *dbr.Session {
	return d.DB.NewSession(nil)
}
