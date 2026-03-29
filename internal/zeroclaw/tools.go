package zeroclaw

// This file defines the canonical catalog of all tools available to ZeroClaw agents.
//
// This is the SINGLE SOURCE OF TRUTH for tool metadata. Both the API response
// (GET /v1/integrations → tools) and the ZeroClaw auto_approve list are derived
// from this catalog. Adding a tool here automatically makes it:
//   - Visible in the mobile app's tools screen
//   - Auto-approved in ZeroClaw's autonomy system
//
// To add a new tool:
//   1. Add an entry to defaultToolCatalog below
//   2. That's it — the API and auto_approve list pick it up automatically

// ToolCategory groups tools by function for display in the mobile app.
type ToolCategory string

const (
	ToolCategorySearch       ToolCategory = "search"
	ToolCategoryFiles        ToolCategory = "files"
	ToolCategoryMemory       ToolCategory = "memory"
	ToolCategoryScheduling   ToolCategory = "scheduling"
	ToolCategoryNotification ToolCategory = "notification"
	ToolCategoryContext      ToolCategory = "context"
	ToolCategoryUtility      ToolCategory = "utility"
	ToolCategoryIntegration  ToolCategory = "integration"
	ToolCategoryShell        ToolCategory = "shell"
)

// ToolDef describes a single tool in the agent's capability set.
type ToolDef struct {
	// Name is the tool identifier as registered in ZeroClaw (e.g. "web_search_tool").
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

// defaultToolCatalog is the complete list of tools loaded into every ZeroClaw agent.
// Order here determines display order in the mobile app.
var defaultToolCatalog = []ToolDef{
	// --- Search & Web ---
	{"web_search_tool", "Web Search", "Search the internet for current information, news, and real-time data", ToolCategorySearch, "https://cdn.crawbl.com/tools/web-search.png"},
	{"web_fetch", "Web Fetch", "Read and extract content from any webpage URL", ToolCategorySearch, "https://cdn.crawbl.com/tools/web-fetch.png"},
	{"http_request", "HTTP Request", "Make HTTP API calls to external services", ToolCategorySearch, "https://cdn.crawbl.com/tools/http-request.png"},

	// --- Files ---
	{"file_read", "Read Files", "Read files from the agent's workspace", ToolCategoryFiles, "https://cdn.crawbl.com/tools/file-read.png"},
	{"file_write", "Write Files", "Create and write files in the agent's workspace", ToolCategoryFiles, "https://cdn.crawbl.com/tools/file-write.png"},
	{"file_edit", "Edit Files", "Edit existing files in the agent's workspace", ToolCategoryFiles, "https://cdn.crawbl.com/tools/file-edit.png"},
	{"glob_search", "File Search", "Search for files by name pattern in the workspace", ToolCategoryFiles, "https://cdn.crawbl.com/tools/glob-search.png"},
	{"content_search", "Content Search", "Search inside files for specific text or patterns", ToolCategoryFiles, "https://cdn.crawbl.com/tools/content-search.png"},

	// --- Memory ---
	{"memory_store", "Remember", "Store important information for future conversations", ToolCategoryMemory, "https://cdn.crawbl.com/tools/memory-store.png"},
	{"memory_recall", "Recall", "Retrieve previously stored information and context", ToolCategoryMemory, "https://cdn.crawbl.com/tools/memory-recall.png"},
	{"memory_forget", "Forget", "Remove stored information from memory", ToolCategoryMemory, "https://cdn.crawbl.com/tools/memory-forget.png"},

	// --- Scheduling ---
	{"cron_add", "Schedule Task", "Create scheduled or recurring tasks", ToolCategoryScheduling, "https://cdn.crawbl.com/tools/cron-add.png"},
	{"cron_list", "List Schedules", "View all scheduled tasks", ToolCategoryScheduling, "https://cdn.crawbl.com/tools/cron-list.png"},
	{"cron_remove", "Remove Schedule", "Delete a scheduled task", ToolCategoryScheduling, "https://cdn.crawbl.com/tools/cron-remove.png"},
	{"cron_update", "Update Schedule", "Modify an existing scheduled task", ToolCategoryScheduling, "https://cdn.crawbl.com/tools/cron-update.png"},
	{"cron_run", "Run Now", "Execute a scheduled task immediately", ToolCategoryScheduling, "https://cdn.crawbl.com/tools/cron-run.png"},
	{"cron_runs", "Run History", "View execution history for a scheduled task", ToolCategoryScheduling, "https://cdn.crawbl.com/tools/cron-runs.png"},

	// --- Orchestrator MCP: Notifications ---
	{"orchestrator__send_push_notification", "Push Notification", "Send push notifications to your mobile device", ToolCategoryNotification, "https://cdn.crawbl.com/tools/push-notification.png"},

	// --- Orchestrator MCP: User Context ---
	{"orchestrator__get_user_profile", "User Profile", "Access your profile information and preferences", ToolCategoryContext, "https://cdn.crawbl.com/tools/user-profile.png"},
	{"orchestrator__get_workspace_info", "Workspace Info", "Get workspace details and agent list", ToolCategoryContext, "https://cdn.crawbl.com/tools/workspace-info.png"},
	{"orchestrator__list_conversations", "Conversations", "List all conversations in your workspace", ToolCategoryContext, "https://cdn.crawbl.com/tools/conversations.png"},
	{"orchestrator__search_past_messages", "Search Messages", "Search through past conversation messages", ToolCategoryContext, "https://cdn.crawbl.com/tools/search-messages.png"},

	// --- Utility ---
	{"calculator", "Calculator", "Perform mathematical calculations", ToolCategoryUtility, "https://cdn.crawbl.com/tools/calculator.png"},
	{"weather", "Weather", "Get current weather information for any location", ToolCategoryUtility, "https://cdn.crawbl.com/tools/weather.png"},
	{"image_info", "Image Info", "Analyze and extract information from images", ToolCategoryUtility, "https://cdn.crawbl.com/tools/image-info.png"},
	{"shell", "Shell Commands", "Run shell commands in the agent's environment", ToolCategoryShell, "https://cdn.crawbl.com/tools/shell.png"},
}

// DefaultToolCatalog returns the full tool catalog for API responses.
func DefaultToolCatalog() []ToolDef {
	return defaultToolCatalog
}

// DefaultAutoApproveList returns tool names for ZeroClaw's autonomy auto_approve config.
// Derived from the catalog — no separate list to maintain.
func DefaultAutoApproveList() []string {
	names := make([]string, 0, len(defaultToolCatalog))
	for _, t := range defaultToolCatalog {
		names = append(names, t.Name)
	}
	return names
}
