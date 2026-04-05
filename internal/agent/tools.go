package agent

// This file defines the canonical catalog of all tools available to agents.
//
// The SINGLE SOURCE OF TRUTH for tool metadata now lives in the seed package
// (migrations/orchestrator/seed/). This file delegates to seed accessors
// and maps to the domain types used by the rest of internal/.

import (
	"github.com/Crawbl-AI/crawbl-backend/migrations/orchestrator/seed"
)

// ToolNameDelegate is the canonical name of the delegation tool.
const ToolNameDelegate = "delegate"

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

// CategoryMeta holds the display metadata for a tool category.
type CategoryMeta struct {
	ID       ToolCategory
	Name     string
	ImageURL string
}

// DefaultToolCatalog returns the full tool catalog for API responses.
func DefaultToolCatalog() []ToolDef {
	entries := seed.Tools()
	out := make([]ToolDef, len(entries))
	for i, e := range entries {
		out[i] = ToolDef{
			Name:        e.Name,
			DisplayName: e.DisplayName,
			Description: e.Description,
			Category:    ToolCategory(e.Category),
			IconURL:     e.IconURL,
		}
	}
	return out
}

// ToolCategories returns display metadata for all tool categories.
func ToolCategories() []CategoryMeta {
	entries := seed.ToolCategories()
	out := make([]CategoryMeta, len(entries))
	for i, e := range entries {
		out[i] = CategoryMeta{
			ID:       ToolCategory(e.ID),
			Name:     e.Name,
			ImageURL: e.ImageURL,
		}
	}
	return out
}

// CategoryMetaByID returns the category display metadata map keyed by category ID.
// This is used by the tools repo to resolve category metadata for tool rows.
func CategoryMetaByID() map[string]CategoryMeta {
	entries := seed.ToolCategories()
	out := make(map[string]CategoryMeta, len(entries))
	for _, e := range entries {
		out[e.ID] = CategoryMeta{
			ID:       ToolCategory(e.ID),
			Name:     e.Name,
			ImageURL: e.ImageURL,
		}
	}
	return out
}
