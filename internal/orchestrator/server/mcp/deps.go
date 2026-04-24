package mcp

import (
	"context"
	"log/slog"
	"regexp"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/auditrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
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
	ctxKeyUserID         contextKey = "mcp_user_id"
	ctxKeyWorkspaceID    contextKey = "mcp_workspace_id"
	ctxKeySessionID      contextKey = "mcp_session_id"
	ctxKeyAPICalls       contextKey = "mcp_api_calls"
	ctxKeyConversationID contextKey = "mcp_conversation_id"
)

// auditLogWriter is the local interface for MCP audit logging.
type auditLogWriter interface {
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
	AuditService auditLogWriter

	// Memory palace dependencies.
	DrawerRepo   drawerStore
	KG           kgStore
	MemoryStack  layers.Stack
	PalaceGraph  palaceGraphStore
	IdentityRepo identitySetter
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

// conversationIDFromContext returns the active conversation ID stamped
// onto the request context by the auth middleware from the runtime's
// X-Conversation-Id header. Tool handlers prefer this value over any
// explicit conversation_id passed in the tool input — the runtime is
// authoritative; the LLM is not.
func conversationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyConversationID).(string)
	return v
}

func contextWithIdentity(ctx context.Context, userID, workspaceID, sessionID, conversationID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyUserID, userID)
	ctx = context.WithValue(ctx, ctxKeyWorkspaceID, workspaceID)
	ctx = context.WithValue(ctx, ctxKeySessionID, sessionID)
	if conversationID != "" {
		ctx = context.WithValue(ctx, ctxKeyConversationID, conversationID)
	}
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

// drawerStore is the drawer subset MCP memory tools use across the
// status, list-wings, list-rooms, taxonomy, search, duplicate, add,
// delete, and diary-read surfaces.
type drawerStore interface {
	Count(ctx context.Context, sess database.SessionRunner, workspaceID string) (int, error)
	ListWings(ctx context.Context, sess database.SessionRunner, workspaceID string) ([]memory.WingCount, error)
	ListRooms(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) ([]memory.RoomCount, error)
	Search(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, wing, room string, limit int) ([]memory.DrawerSearchResult, error)
	CheckDuplicate(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32, threshold float64, limit int) ([]memory.DrawerSearchResult, error)
	Add(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error
	AddIdempotent(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error
	Delete(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error
	BoostImportance(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, delta, maxImportance float64) error
	GetByWingRoom(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) ([]memory.Drawer, error)
}

// kgStore is the knowledge-graph subset MCP tools use for entity
// queries, triple additions, invalidation, timelines, and stats.
type kgStore interface {
	QueryEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, asOf, direction string) ([]memory.TripleResult, error)
	AddTriple(ctx context.Context, sess database.SessionRunner, workspaceID string, t *memory.Triple) (string, error)
	Invalidate(ctx context.Context, sess database.SessionRunner, workspaceID, subject, predicate, object, ended string) error
	Timeline(ctx context.Context, sess database.SessionRunner, workspaceID, entityName string) ([]memory.TripleResult, error)
	Stats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.KGStats, error)
}

// palaceGraphStore is the navigation subset MCP tools use for graph
// traversal and bridge detection.
type palaceGraphStore interface {
	Traverse(ctx context.Context, sess database.SessionRunner, workspaceID, startRoom string, maxHops int) ([]memory.TraversalResult, error)
	FindTunnels(ctx context.Context, sess database.SessionRunner, workspaceID, wingA, wingB string) ([]memory.Tunnel, error)
	GraphStats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.PalaceGraphStats, error)
}

// identitySetter is the identity subset MCP tools use to pin a
// workspace's identity text.
type identitySetter interface {
	Set(ctx context.Context, sess database.SessionRunner, workspaceID, content string) error
}
