package mcp

import sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

// registerTools adds all MCP tools to the server.
func registerTools(server *sdkmcp.Server, deps *Deps) {
	// Push notifications
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "send_push_notification",
		Description: "Send a push notification to the user's mobile device. Use for completed tasks, reminders, or important updates.",
	}, newPushHandler(deps))

	// User context
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "get_user_profile",
		Description: "Get the current user's profile: name, email, nickname, preferences.",
	}, newUserProfileHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "get_workspace_info",
		Description: "Get the current workspace name, agents, and creation date.",
	}, newWorkspaceInfoHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "list_conversations",
		Description: "List all conversations in the current workspace with titles, types, and timestamps.",
	}, newListConversationsHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "search_past_messages",
		Description: "Search through past messages in a conversation by keyword. Use to recall what the user discussed before.",
	}, newSearchMessagesHandler(deps))

	// Agent history
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "create_agent_history",
		Description: "Create a conversation history entry for a delegate agent. Use this when you delegate significant work to an agent and want to record it as a notable event in their history. Do not create entries for every message — only for important tasks, completions, or milestones.",
	}, newCreateAgentHistoryHandler(deps))

	// Agent-to-agent messaging
	if deps.MCPService != nil {
		sdkmcp.AddTool(server, &sdkmcp.Tool{
			Name: "send_message_to_agent",
			Description: "Send a message to another agent in your workspace and get their response. " +
				"Use this to collaborate with other agents on tasks. " +
				"The target agent will receive your message and respond with their result.",
		}, newSendMessageHandler(deps))
	}

	// Shared artifacts
	registerArtifactTools(server, deps)

	// Workflow engine
	registerWorkflowTools(server, deps)

	// Memory palace
	registerMemoryTools(server, deps)
}
