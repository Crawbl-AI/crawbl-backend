// Package tools defines the agent tool catalog for the crawbl-agent-runtime.
//
// Catalog DATA (names, display names, descriptions, icons, categories,
// implementation flags) lives in migrations/orchestrator/seed/tools.json
// and tool_categories.json. This package is a thin Go-typed wrapper
// over the seed loader so callers can keep using the existing ToolDef
// shape and helper functions.
//
// Implementation lives in subpackages:
//
//   - tools/local — tools executed inside the runtime process
//     (web_fetch, web_search_tool, file_read, file_write). Additional
//     local tools will drop in here as they are implemented.
//   - tools/mcp   — tools that bridge to the orchestrator's MCP server
//     at /mcp/v1. The runtime never implements these locally; it
//     forwards every call to the orchestrator with an HMAC bearer token.
//
// To add a tool, append an entry to migrations/orchestrator/seed/tools.json
// with `"implemented": false`, land the implementation in
// tools/local/ or tools/mcp/, then flip the flag to true. The
// /v1/integrations endpoint filters on the flag so users never see a
// "coming soon" tool as if they could call it today.
package tools

import (
	"github.com/Crawbl-AI/crawbl-backend/migrations/orchestrator/seed"
)

// DefaultCatalog returns the full tool catalog, including entries
// flagged as not yet implemented. Used for seeding the orchestrator
// tools table (which holds the full roadmap) and for planning /
// documentation surfaces.
func DefaultCatalog() []ToolDef {
	return toDefs(seed.DefaultTools())
}

// ImplementedCatalog returns only the tools the runtime can actually
// invoke today. Every user-facing API surface should call this
// instead of DefaultCatalog.
func ImplementedCatalog() []ToolDef {
	return toDefs(seed.ImplementedTools())
}

// DefaultAutoApproveList returns tool names for the agent autonomy
// auto-approval set. Only IMPLEMENTED tools are included — there is
// no value in auto-approving a tool that cannot run.
func DefaultAutoApproveList() []string {
	impl := seed.ImplementedTools()
	names := make([]string, 0, len(impl))
	for _, t := range impl {
		names = append(names, t.Name)
	}
	return names
}

// ToolCategories returns display metadata for all tool categories.
// Order matches the seed file.
func ToolCategories() []CategoryMeta {
	cats := seed.ToolCategoriesList()
	out := make([]CategoryMeta, 0, len(cats))
	for _, c := range cats {
		out = append(out, CategoryMeta{
			ID:       ToolCategory(c.ID),
			Name:     c.Name,
			ImageURL: c.ImageURL,
		})
	}
	return out
}

// toDefs converts the seed package's ToolEntry slice into the
// Go-typed ToolDef slice the rest of the codebase expects.
func toDefs(entries []seed.ToolEntry) []ToolDef {
	out := make([]ToolDef, 0, len(entries))
	for _, e := range entries {
		out = append(out, ToolDef{
			Name:        e.Name,
			DisplayName: e.DisplayName,
			Description: e.Description,
			Category:    ToolCategory(e.Category),
			IconURL:     e.IconURL,
			Implemented: e.Implemented,
		})
	}
	return out
}
