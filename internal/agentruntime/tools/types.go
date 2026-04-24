package tools

import (
	"context"
	"sync"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/storage"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools/local"
)

// Tool name constants. These are the canonical identifiers used in gRPC
// ToolCallEvent.tool, agent.tool socket events, and the tools table.
// Every tool name that appears in Go logic MUST be a constant here.
const (
	// ADK built-in: Manager delegates to a sub-agent.
	// ArgField is the JSON key carrying the target agent slug.
	ToolTransferToAgent         = "transfer_to_agent"
	ToolTransferToAgentArgField = "agent_name"

	// Search & Web
	ToolWebSearch   = "web_search_tool"
	ToolWebFetch    = "web_fetch"
	ToolHTTPRequest = "http_request"

	// Files
	ToolFileRead      = "file_read"
	ToolFileWrite     = "file_write"
	ToolFileEdit      = "file_edit"
	ToolGlobSearch    = "glob_search"
	ToolContentSearch = "content_search"

	// Scheduling
	ToolCronAdd    = "cron_add"
	ToolCronList   = "cron_list"
	ToolCronRemove = "cron_remove"
	ToolCronUpdate = "cron_update"
	ToolCronRun    = "cron_run"
	ToolCronRuns   = "cron_runs"

	// Utility
	ToolCalculator = "calculator"
	ToolWeather    = "weather"
	ToolImageInfo  = "image_info"
	ToolShell      = "shell"

	// Orchestrator MCP
	ToolSendPushNotification = "send_push_notification"
	ToolGetUserProfile       = "get_user_profile"
	ToolGetWorkspaceInfo     = "get_workspace_info"
	ToolListConversations    = "list_conversations"
	ToolSearchPastMessages   = "search_past_messages"
	ToolCreateAgentHistory   = "create_agent_history"
	ToolSendMessageToAgent   = "send_message_to_agent"
	ToolCreateArtifact       = "create_artifact"
	ToolReadArtifact         = "read_artifact"
	ToolUpdateArtifact       = "update_artifact"
	ToolReviewArtifact       = "review_artifact"
	ToolCreateWorkflow       = "create_workflow"
	ToolTriggerWorkflow      = "trigger_workflow"
	ToolCheckWorkflowStatus  = "check_workflow_status"
	ToolListWorkflows        = "list_workflows"
	ToolAskQuestions         = "ask_questions"
)

// ToolQueryField maps tool names to the JSON arg field(s) that contain
// the human-readable summary for agent.tool events. Checked in order;
// first non-empty match wins. Tools not in this map get an empty query
// — the mobile app uses tool name + args for l10n.
var ToolQueryField = map[string][]string{
	ToolWebSearch:          {"query"},
	ToolWebFetch:           {"url"},
	ToolHTTPRequest:        {"url"},
	ToolGlobSearch:         {"pattern"},
	ToolContentSearch:      {"query", "pattern"},
	ToolCalculator:         {"expression"},
	ToolWeather:            {"location"},
	ToolShell:              {"command"},
	ToolSearchPastMessages: {"query"},
}

// ToolCategory groups tools by function for display in the mobile
// app. Values are intentionally the same strings the seed file uses
// so JSON round-trips are byte-compatible.
type ToolCategory string

const (
	CategorySearch       ToolCategory = "search"
	CategoryFiles        ToolCategory = "files"
	CategoryMemory       ToolCategory = "memory"
	CategoryScheduling   ToolCategory = "scheduling"
	CategoryNotification ToolCategory = "notification"
	CategoryContext      ToolCategory = "context"
	CategoryUtility      ToolCategory = "utility"
	CategoryIntegration  ToolCategory = "integration"
	CategoryShell        ToolCategory = "shell"
)

// ToolDef describes a single tool in the agent's capability set.
// Mirrors the legacy shape so every existing caller compiles
// unchanged after the seed migration.
type ToolDef struct {
	Name        string
	DisplayName string
	Description string
	Category    ToolCategory
	IconURL     string
	// Implemented tracks whether the runtime currently has a working
	// binding. Callers that surface tools to end users should filter
	// on this field (or use ImplementedCatalog) so "coming soon"
	// tools do not appear as invocable.
	Implemented bool
}

// CategoryMeta holds the display metadata for a tool category.
type CategoryMeta struct {
	ID       ToolCategory
	Name     string
	ImageURL string
}

// webFetchArgs is the LLM-facing input schema for the web_fetch tool. Field tags
// carry both the JSON wire name and the jsonschema description that functiontool
// turns into the tool's argument documentation.
//
// Deliberately kept close to the local.WebFetchOptions shape so the builder stays
// a one-line adapter; the only reason this type exists instead of reusing
// local.WebFetchOptions directly is the jsonschema tag layer, which would otherwise
// leak LLM-ergonomics concerns into the storage/memory packages.
type webFetchArgs struct {
	URL            string `json:"url" jsonschema:"HTTP(S) URL to fetch; required"`
	MaxBytes       int64  `json:"max_bytes,omitempty" jsonschema:"optional cap on response body bytes (default 2 MiB)"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"optional request timeout in seconds (default 10)"`
}

type webFetchResult struct {
	URL  string `json:"url"`
	Body string `json:"body"`
}

type webSearchArgs struct {
	Query      string `json:"query" jsonschema:"free-text search query; required"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"optional cap on returned results (default 5, max 15)"`
}

type webSearchResult struct {
	Query   string                  `json:"query"`
	Results []local.WebSearchResult `json:"results"`
}

type fileReadArgs struct {
	Key string `json:"key" jsonschema:"object key under the workspace, e.g. uploads/trip.md; required"`
}

type fileWriteArgs struct {
	Key         string `json:"key" jsonschema:"object key under the workspace, e.g. drafts/email.md; required"`
	Content     string `json:"content" jsonschema:"file body; required"`
	ContentType string `json:"content_type,omitempty" jsonschema:"optional MIME type (default text/plain)"`
}

// handlerFunc is the shape every local-tool adapter implements: take
// a context + typed args, return typed result + error. Each builder
// below is one of these wrapped through buildFunctionTool.
type handlerFunc[A, R any] func(ctx context.Context, args A) (R, error)

// CommonToolDeps carries the backend handles every local tool needs.
// main.go builds this once per pod from config + constructed stores
// and hands it to runner.BuildOptions.LocalTools via BuildCommonTools.
type CommonToolDeps struct {
	// WorkspaceID is the Crawbl workspace this pod serves. The
	// file tools scope every read and write to this workspace via
	// closure capture.
	WorkspaceID string
	// SearXNGEndpoint is the base URL of the internal meta-search
	// instance. Captured at construction time by web_search_tool.
	SearXNGEndpoint string
	// Spaces is the DigitalOcean Spaces client that backs the
	// file_read / file_write tools. May be nil when storage is not
	// configured (local dev) — BuildCommonTools skips the file tools
	// in that case rather than returning an error, so the rest of
	// the tool set stays available.
	Spaces *storage.SpacesClient
}

var (
	toolDescOnce sync.Once
	toolDescMap  map[string]string
)
