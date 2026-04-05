// Package seed is the single source of truth for all hardcoded configuration
// data in the orchestrator. It embeds JSON files at compile time and exposes
// typed accessor functions. This package must NOT import anything from
// internal/ — it is a pure data package.
package seed

import (
	_ "embed"
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

// AgentEntry defines a default agent blueprint.
type AgentEntry struct {
	Name         string   `json:"name"`
	Slug         string   `json:"slug"`
	Role         string   `json:"role"`
	SystemPrompt string   `json:"system_prompt"`
	Description  string   `json:"description"`
	AllowedTools []string `json:"allowed_tools"`
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

var (
	agents                []AgentEntry
	models                []ModelEntry
	integrations          []IntegrationEntry
	integrationCategories []IntegrationCategoryEntry
)

func init() {
	mustParse(agentsJSON, &agents, "agents.json")
	mustParse(modelsJSON, &models, "models.json")
	mustParse(integrationsJSON, &integrations, "integrations.json")
	mustParse(integrationCategoriesJSON, &integrationCategories, "integration_categories.json")
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

