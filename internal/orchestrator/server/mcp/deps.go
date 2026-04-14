package mcp

import (
	"context"
	"log/slog"
	"regexp"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/auditrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/defaults"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
)

// mcpServerVersion is the version string reported in the MCP implementation info.
const mcpServerVersion = "1.0.0"

// mcpToolCallMethod is the MCP protocol method name for tool invocations.
const mcpToolCallMethod = "tools/call"

var (
	// auditWriteTimeout is the maximum time allowed for writing an audit log entry.
	auditWriteTimeout = defaults.ShortTimeout
)

// auditMaxResponseBytes caps the output stored in audit logs.
const auditMaxResponseBytes = 4096

// defaultSearchLimit is the default number of messages returned by search tools.
const defaultSearchLimit = 20

// maxSearchLimit is the maximum number of messages returned by search tools.
const maxSearchLimit = 50

// maxArtifactContentLength caps artifact content stored via MCP tools (512 KiB).
const maxArtifactContentLength = 512 * 1024

// maxWorkflowStepsLength caps workflow steps JSON stored via MCP tools (128 KiB).
const maxWorkflowStepsLength = 128 * 1024

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
// Memory repo fields are typed against the consumer-side interfaces
// declared in ports.go so this package never imports producer-owned
// repo interfaces.
type Deps struct {
	DB           *dbr.Connection
	Logger       *slog.Logger
	SigningKey   string
	MCPService   mcpservice.Service
	AuditService auditService

	// Memory palace dependencies.
	DrawerRepo   drawerStore
	KG           kgStore
	MemoryStack  layers.Stack
	PalaceGraph  palaceGraphStore
	IdentityRepo identityStore
	Classifier   extract.Classifier
	Embedder     embed.Embedder

	// Noise filter — rejects trivial content (greetings, short filler)
	// from the memory_add_drawer tool. Compiled from noise_patterns.json.
	NoiseMinLength int
	NoisePattern   *regexp.Regexp
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
