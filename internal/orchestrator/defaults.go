// Package orchestrator — this file contains default values, data-driven
// registries, and pure-function helpers derived from the domain types declared
// in types.go. Keep types.go for struct/enum/constant declarations; add
// anything that is a computed default or lookup table here.
package orchestrator

// DefaultAgentModel is the model ID assigned to new agents.
// "auto" means the platform selects the best model (currently gpt-5-mini, future: AWS Bedrock routing).
const DefaultAgentModel = "auto"

// APIVersion is the current version of the orchestrator API.
const APIVersion = "1.0.0"

// DefaultSubscriptionName is the display name for the default subscription tier.
const DefaultSubscriptionName = "Freemium"

// DefaultSubscriptionCode is the internal code for the default subscription tier.
const DefaultSubscriptionCode = "freemium"

// AvailableModels is the registry of models users can choose from.
var AvailableModels = []AgentModelDef{
	{ID: "auto", Name: "Auto", Description: "Platform selects the best model automatically"},
	{ID: "gpt-5-mini", Name: "GPT-5 Mini", Description: "Fast and efficient model for everyday tasks"},
}

// DefaultAgentBlueprint defines the configuration for a default agent in a new workspace.
type DefaultAgentBlueprint struct {
	// Name is the display name of the agent.
	Name string

	// Slug is the routing identifier.
	Slug string

	// Role is the swarm hierarchy role.
	Role string

	// SystemPrompt is the LLM system message for this agent's personality.
	SystemPrompt string

	// Description is a short human-readable summary of the agent's purpose.
	Description string

	// AllowedTools is the list of tool name strings this agent is permitted to use.
	AllowedTools []string
}

// DefaultAgents is the list of agents created by default in new workspaces.
var DefaultAgents = []DefaultAgentBlueprint{
	{
		Name:         "Manager",
		Slug:         "manager",
		Role:         AgentRoleManager,
		SystemPrompt: "You are Manager, the coordinator of this group chat. " +
			"Your PRIMARY job is to delegate tasks to your sub-agents using the delegate tool. " +
			"When the user asks something that sub-agents can handle, delegate to them — do NOT answer yourself. " +
			"When you delegate, your response should ONLY contain your own brief synthesis or follow-up question — " +
			"do NOT repeat or summarize what the sub-agents said, they have their own messages visible to the user. " +
			"Only answer directly for simple coordination questions (\"who are you?\", \"what can you do?\"). " +
			"If you delegated and have nothing original to add, respond with [SILENT]. " +
			"Stay calm, decisive, and brief. Never respond to messages from other agents — avoid feedback loops.",
		Description: "Your swarm coordinator. Delegates tasks and manages the team.",
		AllowedTools: []string{
			"web_search_tool", "web_fetch", "file_read", "file_write",
			"memory_recall", "memory_store", "delegate",
			"orchestrator__send_push_notification",
			"orchestrator__get_user_profile", "orchestrator__get_workspace_info",
			"orchestrator__list_conversations", "orchestrator__search_past_messages",
			"orchestrator__create_agent_history",
			"orchestrator__send_message_to_agent",
			"orchestrator__create_artifact", "orchestrator__read_artifact",
			"orchestrator__update_artifact", "orchestrator__review_artifact",
			"orchestrator__create_workflow", "orchestrator__trigger_workflow",
			"orchestrator__check_workflow_status", "orchestrator__list_workflows",
		},
	},
	{
		Name:         "Wally",
		Slug:         "wally",
		Role:         AgentRoleSubAgent,
		SystemPrompt: "You are Wally, a versatile research and analysis specialist. " +
			"Only speak when you have a relevant opinion, insight, or something genuinely helpful. " +
			"Keep replies short and direct — 1-3 sentences. " +
			"If the topic isn't relevant to you or you have nothing to add, respond with [SILENT]. " +
			"Real people don't reply to every message. " +
			"Never respond to messages from other agents unless the user explicitly asks you to — avoid feedback loops.",
		Description: "A versatile assistant that handles research, writing, analysis, and general help.",
		AllowedTools: []string{
			"web_search_tool", "web_fetch", "file_read", "file_write",
			"memory_recall", "memory_store",
			"orchestrator__send_push_notification",
			"orchestrator__get_user_profile", "orchestrator__get_workspace_info",
			"orchestrator__list_conversations", "orchestrator__search_past_messages",
			"orchestrator__send_message_to_agent",
			"orchestrator__create_artifact", "orchestrator__read_artifact",
			"orchestrator__update_artifact", "orchestrator__review_artifact",
		},
	},
	{
		Name:         "Eve",
		Slug:         "eve",
		Role:         AgentRoleSubAgent,
		SystemPrompt: "You are Eve, a creative and communication specialist. " +
			"Reply only when you have something creative, empathetic, or clarifying to add. " +
			"Ask questions back to the group naturally. Be clear and concise. " +
			"If you have nothing useful to add, respond with [SILENT]. " +
			"Silence is normal — real people don't reply to every message. " +
			"Never respond to messages from other agents unless the user explicitly asks you to — avoid feedback loops.",
		Description: "A creative and communication specialist that handles content creation, email drafting, brainstorming, summarization, and presentation prep.",
		AllowedTools: []string{
			"web_search_tool", "web_fetch", "file_read", "file_write",
			"memory_recall", "memory_store",
			"orchestrator__send_push_notification",
			"orchestrator__get_user_profile", "orchestrator__get_workspace_info",
			"orchestrator__list_conversations", "orchestrator__search_past_messages",
			"orchestrator__send_message_to_agent",
			"orchestrator__create_artifact", "orchestrator__read_artifact",
			"orchestrator__update_artifact", "orchestrator__review_artifact",
		},
	},
}

// CategoryMeta holds display metadata for an item category.
// Used by the handler to build the categories list for GET /v1/integrations.
type CategoryMeta struct {
	ID       string
	Name     string
	ImageURL string
}

// IntegrationCategories returns display metadata for integration (app) categories.
// Tool categories live in the agent package; these are merged at the handler level.
func IntegrationCategories() []CategoryMeta {
	return []CategoryMeta{
		{"communication", "Communication", "https://cdn.crawbl.com/categories/communication.png"},
		{"productivity", "Productivity", "https://cdn.crawbl.com/categories/productivity.png"},
		{"development", "Development", "https://cdn.crawbl.com/categories/development.png"},
	}
}

// ResolveRuntimeState determines the runtime state based on Kubernetes phase
// and verification status. A verified swarm is always ready. Otherwise, the
// state is derived from the phase:
//   - Pending, Progressing, Deleting, or empty -> Provisioning
//   - Error -> Failed
//   - Suspended or unknown -> Offline
func ResolveRuntimeState(phase string, verified bool) RuntimeState {
	if verified {
		return RuntimeStateReady
	}

	switch phase {
	case string(RuntimePhasePending), string(RuntimePhaseProgressing), string(RuntimePhaseDeleting), "":
		return RuntimeStateProvisioning
	case string(RuntimePhaseError):
		return RuntimeStateFailed
	case string(RuntimePhaseSuspended):
		return RuntimeStateOffline
	default:
		return RuntimeStateOffline
	}
}
