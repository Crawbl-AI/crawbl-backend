package mcp

import (
	"context"
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/memory/layers"
	memrepo "github.com/Crawbl-AI/crawbl-backend/internal/memory/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/auditrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

// mcpServerVersion is the version string reported in the MCP implementation info.
const mcpServerVersion = "1.0.0"

// mcpToolCallMethod is the MCP protocol method name for tool invocations.
const mcpToolCallMethod = "tools/call"

// auditWriteTimeout is the maximum time allowed for writing an audit log entry.
const auditWriteTimeout = 5 * time.Second

// auditMaxResponseBytes caps the output stored in audit logs.
const auditMaxResponseBytes = 4096

// defaultSearchLimit is the default number of messages returned by search tools.
const defaultSearchLimit = 20

// maxSearchLimit is the maximum number of messages returned by search tools.
const maxSearchLimit = 50

// contextKey is a private type for context value keys to avoid collisions.
type contextKey string

const (
	ctxKeyUserID      contextKey = "mcp_user_id"
	ctxKeyWorkspaceID contextKey = "mcp_workspace_id"
	ctxKeySessionID   contextKey = "mcp_session_id"
	ctxKeyAPICalls    contextKey = "mcp_api_calls"
)

// auditService is the local interface for MCP audit logging.
type auditService interface {
	WriteLog(ctx context.Context, sess *dbr.Session, entry *auditrepo.AuditLogRow) error
}

// Deps holds all dependencies needed by the MCP server and tool handlers.
type Deps struct {
	DB           *dbr.Connection
	Logger       *slog.Logger
	SigningKey   string
	MCPService   mcpservice.Service
	AuditService auditService

	// Memory palace dependencies.
	DrawerRepo   memrepo.DrawerRepo
	KG           memrepo.KGRepo
	MemoryStack  layers.Stack
	PalaceGraph  memrepo.PalaceGraphRepo
	IdentityRepo memrepo.IdentityRepo
	Classifier   extract.Classifier
	Embedder     embed.Embedder
}

// newSession creates a new database session for MCP tool queries.
func (d *Deps) newSession() *dbr.Session {
	return d.DB.NewSession(nil)
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
	calls := make([]string, 0, 4)
	ctx = context.WithValue(ctx, ctxKeyAPICalls, &calls)
	return ctx
}

// RecordAPICall appends an outgoing API call description to the context.
func RecordAPICall(ctx context.Context, call string) {
	v, _ := ctx.Value(ctxKeyAPICalls).(*[]string)
	if v != nil {
		*v = append(*v, call)
	}
}
