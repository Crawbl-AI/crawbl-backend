package agent

// This file defines the canonical catalog of all tools available to agents.
//
// This is the SINGLE SOURCE OF TRUTH for tool metadata. The API response
// (GET /v1/integrations -> tools) is derived from this catalog. Adding a tool
// here automatically makes it visible in the mobile app's tools screen.
//
// To add a new tool:
//   1. Add an entry to defaultToolCatalog below
//   2. That's it — the API picks it up automatically

// ToolCategory groups tools by function for display in the mobile app.
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
type ToolDef struct {
	// Name is the tool identifier as registered in the agent runtime (e.g. "web_search_tool").
	Name string
	// DisplayName is a human-readable name for the mobile UI (e.g. "Web Search").
	DisplayName string
	// Description explains what the tool does in plain English.
	Description string
	// Category groups the tool for UI organization.
	Category ToolCategory
	// IconURL is a CDN URL for the tool's icon, used in the mobile app.
	IconURL string
}

// defaultToolCatalog is the complete list of tools loaded into every agent runtime.
// Order here determines display order in the mobile app.
var defaultToolCatalog = []ToolDef{
	// --- Search & Web ---
	{"web_search_tool", "Web Search", "Search the internet for current information, news, and real-time data", CategorySearch, "https://cdn.crawbl.com/tools/web-search.png"},
	{"web_fetch", "Web Fetch", "Read and extract content from any webpage URL", CategorySearch, "https://cdn.crawbl.com/tools/web-fetch.png"},
	{"http_request", "HTTP Request", "Make HTTP API calls to external services", CategorySearch, "https://cdn.crawbl.com/tools/http-request.png"},

	// --- Files ---
	{"file_read", "Read Files", "Read files from the agent's workspace", CategoryFiles, "https://cdn.crawbl.com/tools/file-read.png"},
	{"file_write", "Write Files", "Create and write files in the agent's workspace", CategoryFiles, "https://cdn.crawbl.com/tools/file-write.png"},
	{"file_edit", "Edit Files", "Edit existing files in the agent's workspace", CategoryFiles, "https://cdn.crawbl.com/tools/file-edit.png"},
	{"glob_search", "File Search", "Search for files by name pattern in the workspace", CategoryFiles, "https://cdn.crawbl.com/tools/glob-search.png"},
	{"content_search", "Content Search", "Search inside files for specific text or patterns", CategoryFiles, "https://cdn.crawbl.com/tools/content-search.png"},

	// --- Memory ---
	{"memory_store", "Remember", "Store important information for future conversations", CategoryMemory, "https://cdn.crawbl.com/tools/memory-store.png"},
	{"memory_recall", "Recall", "Retrieve previously stored information and context", CategoryMemory, "https://cdn.crawbl.com/tools/memory-recall.png"},
	{"memory_forget", "Forget", "Remove stored information from memory", CategoryMemory, "https://cdn.crawbl.com/tools/memory-forget.png"},

	// --- Scheduling ---
	{"cron_add", "Schedule Task", "Create scheduled or recurring tasks", CategoryScheduling, "https://cdn.crawbl.com/tools/cron-add.png"},
	{"cron_list", "List Schedules", "View all scheduled tasks", CategoryScheduling, "https://cdn.crawbl.com/tools/cron-list.png"},
	{"cron_remove", "Remove Schedule", "Delete a scheduled task", CategoryScheduling, "https://cdn.crawbl.com/tools/cron-remove.png"},
	{"cron_update", "Update Schedule", "Modify an existing scheduled task", CategoryScheduling, "https://cdn.crawbl.com/tools/cron-update.png"},
	{"cron_run", "Run Now", "Execute a scheduled task immediately", CategoryScheduling, "https://cdn.crawbl.com/tools/cron-run.png"},
	{"cron_runs", "Run History", "View execution history for a scheduled task", CategoryScheduling, "https://cdn.crawbl.com/tools/cron-runs.png"},

	// --- Orchestrator MCP: Notifications ---
	{"orchestrator__send_push_notification", "Push Notification", "Send push notifications to your mobile device", CategoryNotification, "https://cdn.crawbl.com/tools/push-notification.png"},

	// --- Orchestrator MCP: User Context ---
	{"orchestrator__get_user_profile", "User Profile", "Access your profile information and preferences", CategoryContext, "https://cdn.crawbl.com/tools/user-profile.png"},
	{"orchestrator__get_workspace_info", "Workspace Info", "Get workspace details and agent list", CategoryContext, "https://cdn.crawbl.com/tools/workspace-info.png"},
	{"orchestrator__list_conversations", "Conversations", "List all conversations in your workspace", CategoryContext, "https://cdn.crawbl.com/tools/conversations.png"},
	{"orchestrator__search_past_messages", "Search Messages", "Search through past conversation messages", CategoryContext, "https://cdn.crawbl.com/tools/search-messages.png"},

	// --- Utility ---
	{"calculator", "Calculator", "Perform mathematical calculations", CategoryUtility, "https://cdn.crawbl.com/tools/calculator.png"},
	{"weather", "Weather", "Get current weather information for any location", CategoryUtility, "https://cdn.crawbl.com/tools/weather.png"},
	{"image_info", "Image Info", "Analyze and extract information from images", CategoryUtility, "https://cdn.crawbl.com/tools/image-info.png"},
	{"shell", "Shell Commands", "Run shell commands in the agent's environment", CategoryShell, "https://cdn.crawbl.com/tools/shell.png"},

	// --- Orchestrator MCP: Agent History ---
	{"orchestrator__create_agent_history", "Agent History", "Record notable events in an agent's history", CategoryIntegration, "https://cdn.crawbl.com/tools/agent-history.png"},

	// --- Delegation ---
	{"delegate", "Delegate", "Hand off tasks to specialized sub-agents", CategoryIntegration, "https://cdn.crawbl.com/tools/delegate.png"},

	// --- Orchestrator MCP: Agent Communication (Phase 2) ---
	{"orchestrator__send_message_to_agent", "Agent Message", "Send messages between agents for collaboration", CategoryIntegration, "https://cdn.crawbl.com/tools/agent-message.png"},

	// --- Orchestrator MCP: Artifacts (Phase 3) ---
	{"orchestrator__create_artifact", "Create Artifact", "Create a shared document or code artifact", CategoryIntegration, "https://cdn.crawbl.com/tools/artifact-create.png"},
	{"orchestrator__read_artifact", "Read Artifact", "Read a shared artifact created by any agent", CategoryIntegration, "https://cdn.crawbl.com/tools/artifact-read.png"},
	{"orchestrator__update_artifact", "Update Artifact", "Update a shared artifact with a new version", CategoryIntegration, "https://cdn.crawbl.com/tools/artifact-update.png"},
	{"orchestrator__review_artifact", "Review Artifact", "Review and approve or request changes on an artifact", CategoryIntegration, "https://cdn.crawbl.com/tools/artifact-review.png"},

	// --- Orchestrator MCP: Workflows (Phase 4) ---
	{"orchestrator__create_workflow", "Create Workflow", "Define a multi-step agent workflow", CategoryIntegration, "https://cdn.crawbl.com/tools/workflow-create.png"},
	{"orchestrator__trigger_workflow", "Start Workflow", "Trigger a defined workflow", CategoryIntegration, "https://cdn.crawbl.com/tools/workflow-trigger.png"},
	{"orchestrator__check_workflow_status", "Workflow Status", "Check the status of a running workflow", CategoryIntegration, "https://cdn.crawbl.com/tools/workflow-status.png"},
	{"orchestrator__list_workflows", "List Workflows", "List all available workflows", CategoryIntegration, "https://cdn.crawbl.com/tools/workflow-list.png"},
}

// DefaultToolCatalog returns the full tool catalog for API responses.
func DefaultToolCatalog() []ToolDef {
	return defaultToolCatalog
}

// CategoryMeta holds the display metadata for a tool category.
type CategoryMeta struct {
	ID       ToolCategory
	Name     string
	ImageURL string
}

// ToolCategories returns display metadata for all tool categories.
func ToolCategories() []CategoryMeta {
	return []CategoryMeta{
		{CategorySearch, "Search", "https://cdn.crawbl.com/categories/search.png"},
		{CategoryFiles, "Files", "https://cdn.crawbl.com/categories/files.png"},
		{CategoryMemory, "Memory", "https://cdn.crawbl.com/categories/memory.png"},
		{CategoryScheduling, "Scheduling", "https://cdn.crawbl.com/categories/scheduling.png"},
		{CategoryNotification, "Notification", "https://cdn.crawbl.com/categories/notification.png"},
		{CategoryContext, "Context", "https://cdn.crawbl.com/categories/context.png"},
		{CategoryUtility, "Utility", "https://cdn.crawbl.com/categories/utility.png"},
		{CategoryIntegration, "Integration", "https://cdn.crawbl.com/categories/integration.png"},
		{CategoryShell, "Shell", "https://cdn.crawbl.com/categories/shell.png"},
	}
}
