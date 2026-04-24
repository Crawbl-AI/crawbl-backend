// Package mcp implements the MCP (Model Context Protocol) server that
// agent runtime pods connect to for orchestrator-side tool execution.
package mcp

import (
	"context"
	"log/slog"
	"regexp"

	"github.com/gocraft/dbr/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/embed"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/extract"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/layers"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory/repo/drawerrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/auditrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/mcpservice"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/config"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// Const declarations

// mcpServerVersion is the version string reported in the MCP implementation info.
const mcpServerVersion = "1.0.0"

// mcpToolCallMethod is the MCP protocol method name for tool invocations.
const mcpToolCallMethod = "tools/call"

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

const (
	// errAgentIDOrSlugRequired is returned when a tool requires agent_id or agent_slug but neither is provided.
	errAgentIDOrSlugRequired = "agent_id or agent_slug is required"
)

// Context key constants for MCP request-scoped values.
const (
	ctxKeyUserID         contextKey = "mcp_user_id"
	ctxKeyWorkspaceID    contextKey = "mcp_workspace_id"
	ctxKeySessionID      contextKey = "mcp_session_id"
	ctxKeyAPICalls       contextKey = "mcp_api_calls"
	ctxKeyConversationID contextKey = "mcp_conversation_id"
)

// Memory classifier constants.
const (
	classifierMinConfidence = 0.5
	importanceScale         = 5.0
	defaultImportance       = 3.0
)

// Var declarations

var (
	// auditWriteTimeout is the maximum time allowed for writing an audit log entry.
	auditWriteTimeout = config.ShortTimeout

	// errNoWorkspaceIdentity is returned when the MCP bearer token did not carry workspace context.
	errNoWorkspaceIdentity = errorf("unauthorized: no workspace identity")

	// errNoUserIdentity is returned when the MCP bearer token did not carry user context.
	errNoUserIdentity = errorf("unauthorized: no user identity")
)

// Type declarations

// contextKey is a private type for context value keys to avoid collisions.
type contextKey string

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
	IdentityRepo identitySetter
	Classifier   extract.Classifier
	Embedder     embed.Embedder

	// Noise filter — rejects trivial content (greetings, short filler)
	// from the memory_add_drawer tool. Compiled from noise_patterns.json.
	NoiseMinLength int
	NoisePattern   *regexp.Regexp
}

// authedToolFn is the business logic signature for MCP tools that only need
// a workspace identity. The adapter resolves the workspace ID and opens a
// fresh dbr session before calling fn.
type authedToolFn[I any, O any] func(
	ctx context.Context,
	sess *dbr.Session,
	workspaceID string,
	input I,
) (*sdkmcp.CallToolResult, O, error)

// authedToolUserFn is the business logic signature for MCP tools that need
// both user and workspace identity (e.g. artifact, chat, send_message_to_agent).
type authedToolUserFn[I any, O any] func(
	ctx context.Context,
	sess *dbr.Session,
	userID string,
	workspaceID string,
	input I,
) (*sdkmcp.CallToolResult, O, error)

// Port interface declarations (consumer-side, per project convention)

// drawerStore is the drawer subset MCP memory tools use across the
// status, list-wings, list-rooms, taxonomy, search, duplicate, add,
// delete, and diary-read surfaces.
type drawerStore interface {
	Count(ctx context.Context, sess database.SessionRunner, workspaceID string) (int, error)
	ListWings(ctx context.Context, sess database.SessionRunner, workspaceID string) ([]memory.WingCount, error)
	ListRooms(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) ([]memory.RoomCount, error)
	Search(ctx context.Context, sess database.SessionRunner, opts drawerrepo.SearchOpts) ([]memory.DrawerSearchResult, error)
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

// Tool input types

// createAgentHistoryInput is the input for the create_agent_history tool.
type createAgentHistoryInput struct {
	AgentID        string `json:"agent_id,omitempty"`
	AgentSlug      string `json:"agent_slug,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle,omitempty"`
	Description    string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// pushInput is the input for the send_push_notification tool.
type pushInput struct {
	Title       string `json:"title" jsonschema:"the notification title shown on the device"`
	Message     string `json:"message" jsonschema:"the notification body text"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// userProfileInput is the input for the get_user_profile tool.
type userProfileInput struct {
	IncludePreferences bool   `json:"include_preferences,omitempty" jsonschema:"include user preferences in response"`
	Description        string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// workspaceInfoInput is the input for the get_workspace_info tool.
type workspaceInfoInput struct {
	IncludeAgents bool   `json:"include_agents,omitempty" jsonschema:"include agent list in response"`
	Description   string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// listConversationsInput is the input for the list_conversations tool.
type listConversationsInput struct {
	IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"include archived conversations"`
	Description     string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// searchMessagesInput is the input for the search_past_messages tool.
type searchMessagesInput struct {
	ConversationID string `json:"conversation_id" jsonschema:"ID of the conversation to search in"`
	Query          string `json:"query" jsonschema:"search keyword or phrase"`
	Limit          int    `json:"limit" jsonschema:"maximum results to return (default 20, max 50)"`
	Description    string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// sendMessageInput is the input for the send_message_to_agent tool.
type sendMessageInput struct {
	AgentSlug      string `json:"agent_slug" jsonschema:"slug of the target agent (e.g. 'wally', 'eve')"`
	Message        string `json:"message" jsonschema:"the message/task to send to the target agent"`
	ConversationID string `json:"conversation_id,omitempty" jsonschema:"optional conversation ID for context"`
	Description    string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// createArtifactInput is the input for the create_artifact tool.
type createArtifactInput struct {
	Title          string `json:"title" jsonschema:"the title of the artifact"`
	Content        string `json:"content" jsonschema:"the initial content of the artifact"`
	ContentType    string `json:"content_type,omitempty" jsonschema:"MIME type of the content (default: text/markdown)"`
	ConversationID string `json:"conversation_id,omitempty" jsonschema:"optional conversation to associate the artifact with"`
	AgentID        string `json:"agent_id,omitempty" jsonschema:"UUID of the agent creating the artifact (fast path)"`
	AgentSlug      string `json:"agent_slug,omitempty" jsonschema:"slug of the agent creating the artifact"`
	Description    string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// readArtifactInput is the input for the read_artifact tool.
type readArtifactInput struct {
	ArtifactID  string `json:"artifact_id" jsonschema:"the ID of the artifact to read"`
	Version     int    `json:"version,omitempty" jsonschema:"specific version to read (default: latest)"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// updateArtifactInput is the input for the update_artifact tool.
type updateArtifactInput struct {
	ArtifactID      string `json:"artifact_id" jsonschema:"the ID of the artifact to update"`
	Content         string `json:"content" jsonschema:"the new content for the artifact"`
	ChangeSummary   string `json:"change_summary,omitempty" jsonschema:"a brief summary of what changed"`
	ExpectedVersion int    `json:"expected_version,omitempty" jsonschema:"for optimistic locking — update fails if current version differs"`
	AgentID         string `json:"agent_id,omitempty" jsonschema:"UUID of the agent making the update (fast path)"`
	AgentSlug       string `json:"agent_slug,omitempty" jsonschema:"slug of the agent making the update"`
	Description     string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// reviewArtifactInput is the input for the review_artifact tool.
type reviewArtifactInput struct {
	ArtifactID  string `json:"artifact_id" jsonschema:"the ID of the artifact to review"`
	Outcome     string `json:"outcome" jsonschema:"review outcome: approved, changes_requested, or commented"`
	Comments    string `json:"comments" jsonschema:"review comments explaining the outcome"`
	Version     int    `json:"version,omitempty" jsonschema:"specific version to review (default: current)"`
	AgentID     string `json:"agent_id,omitempty" jsonschema:"UUID of the reviewing agent (fast path)"`
	AgentSlug   string `json:"agent_slug,omitempty" jsonschema:"slug of the reviewing agent"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// createWorkflowInput keeps a single Description field because the
// workflow's own description doubles as the tool-status description
// — both answer "what is this workflow / what am I doing right now"
// with the same sentence. buildWireArgs in chatservice reads
// args["description"] and this existing field maps to exactly that
// JSON key.
type createWorkflowInput struct {
	Name        string `json:"name" jsonschema:"name for the workflow"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing the workflow — shown to the user while the tool runs"`
	Steps       string `json:"steps" jsonschema:"JSON array of workflow steps, each with name, agent_slug, prompt_template, and optional timeout_secs, requires_approval, on_failure, output_key, max_retries"`
}

// triggerWorkflowInput is the input for the trigger_workflow tool.
type triggerWorkflowInput struct {
	WorkflowID     string `json:"workflow_id" jsonschema:"ID of the workflow definition to execute"`
	ConversationID string `json:"conversation_id,omitempty" jsonschema:"optional conversation ID to associate with the execution"`
	InitialContext string `json:"initial_context,omitempty" jsonschema:"optional JSON object with initial template variables for the workflow"`
	Description    string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// checkWorkflowStatusInput is the input for the check_workflow_status tool.
type checkWorkflowStatusInput struct {
	ExecutionID string `json:"execution_id" jsonschema:"ID of the workflow execution to check"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// listWorkflowsInput is the input for the list_workflows tool.
type listWorkflowsInput struct {
	IncludeInactive bool   `json:"include_inactive,omitempty" jsonschema:"include inactive workflows in the list"`
	Description     string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// askQuestionsInput is the input for the ask_questions tool.
type askQuestionsInput struct {
	AgentID        string             `json:"agent_id,omitempty"        jsonschema:"UUID of the asking agent (fast path)"`
	AgentSlug      string             `json:"agent_slug,omitempty"      jsonschema:"slug of the asking agent"`
	ConversationID string             `json:"conversation_id,omitempty" jsonschema:"optional; defaults to the current conversation if the runtime provided it — agents should not set this"`
	Turns          []askQuestionsTurn `json:"turns"                     jsonschema:"ordered list of turn groups"`
	Description    string             `json:"description,omitempty"     jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// askQuestionsTurn is a single turn group within askQuestionsInput.
type askQuestionsTurn struct {
	Label     string                 `json:"label,omitempty"`
	Questions []askQuestionsQuestion `json:"questions"`
}

// askQuestionsQuestion is a single question within askQuestionsTurn.
type askQuestionsQuestion struct {
	Prompt      string               `json:"prompt"`
	Mode        string               `json:"mode"                   jsonschema:"single or multi"`
	Options     []askQuestionsOption `json:"options"                jsonschema:"2-26 options"`
	AllowCustom bool                 `json:"allow_custom,omitempty" jsonschema:"whether the user may also provide free-text input (default false)"`
}

// askQuestionsOption is a single selectable option within askQuestionsQuestion.
type askQuestionsOption struct {
	Label string `json:"label"`
}

// Memory tool input types

type memoryStatusInput struct {
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryListWingsInput struct {
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryListRoomsInput struct {
	Wing        string `json:"wing,omitempty" jsonschema:"optional wing filter"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryGetTaxonomyInput struct {
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memorySearchInput struct {
	Query       string `json:"query" jsonschema:"what to search for"`
	Limit       int    `json:"limit,omitempty" jsonschema:"max results (default 5)"`
	Wing        string `json:"wing,omitempty" jsonschema:"optional wing filter"`
	Room        string `json:"room,omitempty" jsonschema:"optional room filter"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryCheckDuplicateInput struct {
	Content     string  `json:"content" jsonschema:"content to check for duplicates"`
	Threshold   float64 `json:"threshold,omitempty" jsonschema:"similarity threshold (default 0.9)"`
	Description string  `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryTraverseInput struct {
	StartRoom   string `json:"start_room" jsonschema:"room to start traversal from"`
	MaxHops     int    `json:"max_hops,omitempty" jsonschema:"maximum hops (default 3)"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryFindTunnelsInput struct {
	WingA       string `json:"wing_a,omitempty" jsonschema:"first wing filter"`
	WingB       string `json:"wing_b,omitempty" jsonschema:"second wing filter"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryGraphStatsInput struct {
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryAddDrawerInput struct {
	Wing        string `json:"wing" jsonschema:"wing to file the drawer in"`
	Room        string `json:"room" jsonschema:"room within the wing"`
	Content     string `json:"content" jsonschema:"the content to store"`
	SourceFile  string `json:"source_file,omitempty" jsonschema:"optional source file reference"`
	AddedBy     string `json:"added_by,omitempty" jsonschema:"who added this memory"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryDeleteDrawerInput struct {
	DrawerID    string `json:"drawer_id" jsonschema:"ID of the drawer to delete"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memorySetIdentityInput struct {
	Content     string `json:"content" jsonschema:"the identity text for this workspace"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryKGQueryInput struct {
	Entity      string `json:"entity" jsonschema:"entity name to query"`
	AsOf        string `json:"as_of,omitempty" jsonschema:"optional date filter (YYYY-MM-DD)"`
	Direction   string `json:"direction,omitempty" jsonschema:"outgoing, incoming, or both (default both)"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryKGAddInput struct {
	Subject      string `json:"subject" jsonschema:"subject entity name"`
	Predicate    string `json:"predicate" jsonschema:"relationship type"`
	Object       string `json:"object" jsonschema:"object entity name"`
	ValidFrom    string `json:"valid_from,omitempty" jsonschema:"when the fact became true (YYYY-MM-DD)"`
	SourceCloset string `json:"source_closet,omitempty" jsonschema:"source drawer/closet ID"`
	Description  string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryKGInvalidateInput struct {
	Subject     string `json:"subject" jsonschema:"subject entity name"`
	Predicate   string `json:"predicate" jsonschema:"relationship type"`
	Object      string `json:"object" jsonschema:"object entity name"`
	Ended       string `json:"ended,omitempty" jsonschema:"when the fact ended (YYYY-MM-DD, default now)"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryKGTimelineInput struct {
	Entity      string `json:"entity,omitempty" jsonschema:"optional entity to filter timeline"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryKGStatsInput struct {
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryDiaryWriteInput struct {
	AgentName   string `json:"agent_name" jsonschema:"name of the agent writing the diary entry"`
	Entry       string `json:"entry" jsonschema:"the diary entry text"`
	Topic       string `json:"topic,omitempty" jsonschema:"optional topic/tag for the entry"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

type memoryDiaryReadInput struct {
	AgentName   string `json:"agent_name" jsonschema:"name of the agent whose diary to read"`
	LastN       int    `json:"last_n,omitempty" jsonschema:"number of recent entries (default 10)"`
	Description string `json:"description,omitempty" jsonschema:"one short sentence (max 80 chars) in the user's current chat language describing what you are doing; shown to the user while the tool runs"`
}

// errorf is a helper to create package-level error values without heap allocation at call sites.
func errorf(msg string) error {
	return &staticError{msg: msg}
}

// staticError is an immutable error value for package-level sentinel errors.
type staticError struct{ msg string }

func (e *staticError) Error() string { return e.msg }
