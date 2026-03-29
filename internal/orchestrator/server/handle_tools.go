package server

import (
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

// defaultTools returns the list of tools that are always enabled in ZeroClaw.
// These are auto-loaded at agent startup and cannot be toggled off by the user.
// Used by handleIntegrationsList to populate the "tools" section of the response.
func defaultTools() []orchestrator.AgentTool {
	return []orchestrator.AgentTool{
		// --- Search & Web ---
		{Name: "web_search_tool", DisplayName: "Web Search", Description: "Search the internet for current information, news, and real-time data", Category: orchestrator.ToolCategorySearch, Enabled: true},
		{Name: "web_fetch", DisplayName: "Web Fetch", Description: "Read and extract content from any webpage URL", Category: orchestrator.ToolCategorySearch, Enabled: true},
		{Name: "http_request", DisplayName: "HTTP Request", Description: "Make HTTP API calls to external services", Category: orchestrator.ToolCategorySearch, Enabled: true},

		// --- Files ---
		{Name: "file_read", DisplayName: "Read Files", Description: "Read files from the agent's workspace", Category: orchestrator.ToolCategoryFiles, Enabled: true},
		{Name: "file_write", DisplayName: "Write Files", Description: "Create and write files in the agent's workspace", Category: orchestrator.ToolCategoryFiles, Enabled: true},
		{Name: "file_edit", DisplayName: "Edit Files", Description: "Edit existing files in the agent's workspace", Category: orchestrator.ToolCategoryFiles, Enabled: true},
		{Name: "glob_search", DisplayName: "File Search", Description: "Search for files by name pattern in the workspace", Category: orchestrator.ToolCategoryFiles, Enabled: true},
		{Name: "content_search", DisplayName: "Content Search", Description: "Search inside files for specific text or patterns", Category: orchestrator.ToolCategoryFiles, Enabled: true},

		// --- Memory ---
		{Name: "memory_store", DisplayName: "Remember", Description: "Store important information for future conversations", Category: orchestrator.ToolCategoryMemory, Enabled: true},
		{Name: "memory_recall", DisplayName: "Recall", Description: "Retrieve previously stored information and context", Category: orchestrator.ToolCategoryMemory, Enabled: true},
		{Name: "memory_forget", DisplayName: "Forget", Description: "Remove stored information from memory", Category: orchestrator.ToolCategoryMemory, Enabled: true},

		// --- Scheduling ---
		{Name: "cron_add", DisplayName: "Schedule Task", Description: "Create scheduled or recurring tasks", Category: orchestrator.ToolCategoryScheduling, Enabled: true},
		{Name: "cron_list", DisplayName: "List Schedules", Description: "View all scheduled tasks", Category: orchestrator.ToolCategoryScheduling, Enabled: true},
		{Name: "cron_remove", DisplayName: "Remove Schedule", Description: "Delete a scheduled task", Category: orchestrator.ToolCategoryScheduling, Enabled: true},
		{Name: "cron_update", DisplayName: "Update Schedule", Description: "Modify an existing scheduled task", Category: orchestrator.ToolCategoryScheduling, Enabled: true},
		{Name: "cron_run", DisplayName: "Run Now", Description: "Execute a scheduled task immediately", Category: orchestrator.ToolCategoryScheduling, Enabled: true},
		{Name: "cron_runs", DisplayName: "Run History", Description: "View execution history for a scheduled task", Category: orchestrator.ToolCategoryScheduling, Enabled: true},

		// --- Orchestrator MCP: Notifications ---
		{Name: "orchestrator__send_push_notification", DisplayName: "Push Notification", Description: "Send push notifications to your mobile device", Category: orchestrator.ToolCategoryNotification, Enabled: true},

		// --- Orchestrator MCP: User Context ---
		{Name: "orchestrator__get_user_profile", DisplayName: "User Profile", Description: "Access your profile information and preferences", Category: orchestrator.ToolCategoryContext, Enabled: true},
		{Name: "orchestrator__get_workspace_info", DisplayName: "Workspace Info", Description: "Get workspace details and agent list", Category: orchestrator.ToolCategoryContext, Enabled: true},
		{Name: "orchestrator__list_conversations", DisplayName: "Conversations", Description: "List all conversations in your workspace", Category: orchestrator.ToolCategoryContext, Enabled: true},
		{Name: "orchestrator__search_past_messages", DisplayName: "Search Messages", Description: "Search through past conversation messages", Category: orchestrator.ToolCategoryContext, Enabled: true},

		// --- Utility ---
		{Name: "calculator", DisplayName: "Calculator", Description: "Perform mathematical calculations", Category: orchestrator.ToolCategoryUtility, Enabled: true},
		{Name: "weather", DisplayName: "Weather", Description: "Get current weather information for any location", Category: orchestrator.ToolCategoryUtility, Enabled: true},
		{Name: "image_info", DisplayName: "Image Info", Description: "Analyze and extract information from images", Category: orchestrator.ToolCategoryUtility, Enabled: true},
		{Name: "shell", DisplayName: "Shell Commands", Description: "Run shell commands in the agent's environment", Category: orchestrator.ToolCategoryShell, Enabled: true},
	}
}
