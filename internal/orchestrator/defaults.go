// Package orchestrator — this file contains default values, data-driven
// registries, and pure-function helpers derived from the domain types declared
// in types.go. Keep types.go for struct/enum/constant declarations; add
// anything that is a computed default or lookup table here.
package orchestrator

import (
	"github.com/Crawbl-AI/crawbl-backend/migrations/orchestrator/seed"
)

// DefaultAgentModel is the model ID assigned to new agents.
// "auto" means the platform selects the best model (currently gpt-5-mini, future: AWS Bedrock routing).
const DefaultAgentModel = "auto"

// APIVersion is the current version of the orchestrator API.
const APIVersion = "1.0.0"

// DefaultSubscriptionName is the display name for the default subscription tier.
const DefaultSubscriptionName = "Freemium"

// DefaultSubscriptionCode is the internal code for the default subscription tier.
const DefaultSubscriptionCode = "freemium"

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

// GetDefaultAgents returns the list of agents created by default in new workspaces.
// Data is loaded from the seed package (migrations/orchestrator/seed/agents.json).
func GetDefaultAgents() []DefaultAgentBlueprint {
	entries := seed.DefaultAgents()
	out := make([]DefaultAgentBlueprint, len(entries))
	for i, e := range entries {
		out[i] = DefaultAgentBlueprint{
			Name:         e.Name,
			Slug:         e.Slug,
			Role:         e.Role,
			SystemPrompt: e.SystemPrompt,
			Description:  e.Description,
			AllowedTools: e.AllowedTools,
		}
	}
	return out
}

// GetAvailableModels returns the registry of models users can choose from.
// Data is loaded from the seed package (migrations/orchestrator/seed/models.json).
func GetAvailableModels() []AgentModelDef {
	entries := seed.AvailableModels()
	out := make([]AgentModelDef, len(entries))
	for i, e := range entries {
		out[i] = AgentModelDef{
			ID:          e.ID,
			Name:        e.Name,
			Description: e.Description,
		}
	}
	return out
}

// CategoryMeta holds display metadata for an item category.
// Used by the handler to build the categories list for GET /v1/integrations.
type CategoryMeta struct {
	ID       string
	Name     string
	ImageURL string
}

// IntegrationCategories returns display metadata for integration (app) categories.
// Data is loaded from the seed package (migrations/orchestrator/seed/integration_categories.json).
func IntegrationCategories() []CategoryMeta {
	entries := seed.IntegrationCategories()
	out := make([]CategoryMeta, len(entries))
	for i, e := range entries {
		out[i] = CategoryMeta{
			ID:       e.ID,
			Name:     e.Name,
			ImageURL: e.ImageURL,
		}
	}
	return out
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
