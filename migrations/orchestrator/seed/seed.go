// Package seed is the single source of truth for all hardcoded configuration
// data in the orchestrator. It embeds JSON files at compile time and exposes
// typed accessor functions. This package must NOT import anything from
// internal/ — it is a pure data package.
package seed

import (
	_ "embed" // required for //go:embed directives to embed JSON data files at compile time
	"encoding/json"
)

//go:embed agents.json
var agentsJSON []byte

//go:embed models.json
var modelsJSON []byte

//go:embed integrations.json
var integrationsJSON []byte

//go:embed integration_categories.json
var integrationCategoriesJSON []byte

//go:embed tools.json
var toolsJSON []byte

//go:embed tool_categories.json
var toolCategoriesJSON []byte

//go:embed usage_plans.json
var usagePlansJSON []byte

//go:embed model_pricing.json
var modelPricingJSON []byte

// AgentEntry defines a default agent blueprint.
type AgentEntry struct {
	Name         string   `json:"name"`
	Slug         string   `json:"slug"`
	Role         string   `json:"role"`
	SystemPrompt string   `json:"system_prompt"`
	Description  string   `json:"description"`
	AllowedTools []string `json:"allowed_tools"`
}

// ToolEntry defines a single tool in the agent capability catalog.
// Implemented tracks whether the runtime actually has a working
// binding for the tool — mobile / API consumers must filter on this
// flag (or use ImplementedTools()) so users never see "coming soon"
// tools as if they were usable today.
type ToolEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	IconURL     string `json:"icon_url"`
	Implemented bool   `json:"implemented"`
}

// ToolCategoryEntry is the display metadata for a tool category.
type ToolCategoryEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
}

// ModelEntry describes an available LLM model.
type ModelEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// IntegrationEntry describes a third-party integration provider.
type IntegrationEntry struct {
	Provider    string `json:"provider"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	CategoryID  string `json:"category_id"`
	IsEnabled   bool   `json:"is_enabled"`
}

// IntegrationCategoryEntry holds display metadata for an integration category.
type IntegrationCategoryEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
}

// UsagePlanEntry defines a subscription plan with token limits.
type UsagePlanEntry struct {
	PlanID              string `json:"plan_id"`
	Name                string `json:"name"`
	MonthlyTokenLimit   int64  `json:"monthly_token_limit"`
	DailyRequestLimit   *int   `json:"daily_request_limit"`
	MaxTokensPerRequest *int   `json:"max_tokens_per_request"`
}

// ModelPricingEntry holds bootstrap pricing for a model.
type ModelPricingEntry struct {
	Provider           string  `json:"provider"`
	Model              string  `json:"model"`
	Region             string  `json:"region"`
	InputCostPerToken  float64 `json:"input_cost_per_token"`
	OutputCostPerToken float64 `json:"output_cost_per_token"`
	CachedCostPerToken float64 `json:"cached_cost_per_token"`
	Source             string  `json:"source"`
}

var (
	agents                []AgentEntry
	models                []ModelEntry
	integrations          []IntegrationEntry
	integrationCategories []IntegrationCategoryEntry
	tools                 []ToolEntry
	toolCategories        []ToolCategoryEntry
	usagePlans            []UsagePlanEntry
	modelPricing          []ModelPricingEntry
)

func init() {
	mustParse(agentsJSON, &agents, "agents.json")
	mustParse(modelsJSON, &models, "models.json")
	mustParse(integrationsJSON, &integrations, "integrations.json")
	mustParse(integrationCategoriesJSON, &integrationCategories, "integration_categories.json")
	mustParse(toolsJSON, &tools, "tools.json")
	mustParse(toolCategoriesJSON, &toolCategories, "tool_categories.json")
	mustParse(usagePlansJSON, &usagePlans, "usage_plans.json")
	mustParse(modelPricingJSON, &modelPricing, "model_pricing.json")
}

func mustParse(data []byte, target any, name string) {
	if err := json.Unmarshal(data, target); err != nil {
		panic("seed: failed to parse embedded " + name + ": " + err.Error())
	}
}

// DefaultAgents returns the list of agents created by default in new workspaces.
func DefaultAgents() []AgentEntry { return agents }

// AvailableModels returns the registry of models users can choose from.
func AvailableModels() []ModelEntry { return models }

// IntegrationProviders returns all supported third-party integration providers.
func IntegrationProviders() []IntegrationEntry { return integrations }

// IntegrationCategories returns display metadata for integration categories.
func IntegrationCategories() []IntegrationCategoryEntry { return integrationCategories }

// DefaultTools returns the complete tool catalog, including entries
// flagged as not yet implemented. Use this for seeding the tools
// table (where the roadmap lives) or for docs / planning. API
// handlers and user-facing code should prefer ImplementedTools.
func DefaultTools() []ToolEntry { return tools }

// ImplementedTools returns only the subset of tools that the runtime
// can actually invoke today. This is the list the /v1/integrations
// endpoint surfaces so the mobile app never shows a tool the agent
// cannot use.
func ImplementedTools() []ToolEntry {
	out := make([]ToolEntry, 0, len(tools))
	for _, t := range tools {
		if t.Implemented {
			out = append(out, t)
		}
	}
	return out
}

// ToolCategoriesList returns the display metadata for every tool
// category. Named -List to avoid colliding with the orchestrator's
// integration-category accessor.
func ToolCategoriesList() []ToolCategoryEntry { return toolCategories }

// UsagePlans returns the list of available subscription plans.
func UsagePlans() []UsagePlanEntry { return usagePlans }

// ModelPricing returns the bootstrap model pricing entries.
func ModelPricing() []ModelPricingEntry { return modelPricing }
