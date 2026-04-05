// Package seed is the single source of truth for all hardcoded configuration
// data in the orchestrator. It embeds JSON files at compile time and exposes
// typed accessor functions. This package must NOT import anything from
// internal/ — it is a pure data package.
package seed

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"
)

// ---------------------------------------------------------------------------
// Embedded JSON files
// ---------------------------------------------------------------------------

//go:embed tools.json
var toolsJSON []byte

//go:embed catalog.json
var catalogJSON []byte

//go:embed agents.json
var agentsJSON []byte

//go:embed models.json
var modelsJSON []byte

//go:embed integrations.json
var integrationsJSON []byte

//go:embed integration_categories.json
var integrationCategoriesJSON []byte

// ---------------------------------------------------------------------------
// Entry types — JSON-tag-annotated structs for each seed file.
// These are the public types returned by the accessor functions.
// ---------------------------------------------------------------------------

// ToolEntry describes a single tool in the agent capability catalog.
type ToolEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	IconURL     string `json:"icon_url"`
}

// CategoryEntry holds display metadata for a tool category.
type CategoryEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
}

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

// ---------------------------------------------------------------------------
// Package-private parsed data, populated at init time.
// ---------------------------------------------------------------------------

var (
	tools                 []ToolEntry
	toolCategories        []CategoryEntry
	agents                []AgentEntry
	models                []ModelEntry
	integrations          []IntegrationEntry
	integrationCategories []IntegrationCategoryEntry
)

func init() {
	mustParse(toolsJSON, &tools, "tools.json")
	mustParse(catalogJSON, &toolCategories, "catalog.json")
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

// ---------------------------------------------------------------------------
// Public accessors
// ---------------------------------------------------------------------------

// Tools returns the full tool catalog.
func Tools() []ToolEntry { return tools }

// ToolCategories returns display metadata for all tool categories.
func ToolCategories() []CategoryEntry { return toolCategories }

// DefaultAgents returns the list of agents created by default in new workspaces.
func DefaultAgents() []AgentEntry { return agents }

// AvailableModels returns the registry of models users can choose from.
func AvailableModels() []ModelEntry { return models }

// IntegrationProviders returns all supported third-party integration providers.
func IntegrationProviders() []IntegrationEntry { return integrations }

// IntegrationCategories returns display metadata for integration categories.
func IntegrationCategories() []IntegrationCategoryEntry { return integrationCategories }

// ---------------------------------------------------------------------------
// Database seeding — idempotent, safe to call on every startup.
// ---------------------------------------------------------------------------

// Run seeds the orchestrator database with the tool catalog.
// It is idempotent — safe to call on every startup.
func Run(ctx context.Context, sess *dbr.Session, logger *slog.Logger) error {
	if err := seedTools(ctx, sess, logger); err != nil {
		return fmt.Errorf("seed tools: %w", err)
	}
	logger.Info("database seeding completed")
	return nil
}

func seedTools(ctx context.Context, sess *dbr.Session, logger *slog.Logger) error {
	catalog := tools
	now := time.Now().UTC()

	for i, tool := range catalog {
		var existing struct {
			Name string `db:"name"`
		}
		err := sess.Select("name").From("tools").
			Where("name = ?", tool.Name).
			LoadOneContext(ctx, &existing)

		if err == nil {
			// Row exists — update in place.
			_, err = sess.Update("tools").
				Set("display_name", tool.DisplayName).
				Set("description", tool.Description).
				Set("category", tool.Category).
				Set("icon_url", tool.IconURL).
				Set("sort_order", i).
				Where("name = ?", tool.Name).
				ExecContext(ctx)
			if err != nil {
				return fmt.Errorf("update tool %s: %w", tool.Name, err)
			}
		} else {
			// Row missing — insert.
			_, err = sess.InsertInto("tools").
				Pair("name", tool.Name).
				Pair("display_name", tool.DisplayName).
				Pair("description", tool.Description).
				Pair("category", tool.Category).
				Pair("icon_url", tool.IconURL).
				Pair("sort_order", i).
				Pair("created_at", now).
				ExecContext(ctx)
			if err != nil {
				return fmt.Errorf("insert tool %s: %w", tool.Name, err)
			}
		}
	}

	logger.Info("seeded tools", slog.Int("count", len(catalog)))
	return nil
}
