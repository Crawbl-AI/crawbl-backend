// Package tools defines the ADK tool.Tool builders that wrap runtime-local
// Go functions (internal/agentruntime/tools/local/*) as
// google.golang.org/adk/tool/functiontool instruments every llmagent
// can bind directly.
//
// Lives in the tools package (next to catalog.go and tools/local/*)
// because the builders are tool-package concerns, not agent-package
// concerns — the agents package only consumes the []adktool.Tool
// slice via BuildCommonTools below.
//
// Design rules enforced here:
//
//   - Tool names come from the Tool* constants in catalog.go. No
//     literal strings in builder bodies.
//   - Descriptions come from seed/tools.json via seed.DefaultTools()
//     so the mobile integrations screen, the /v1/integrations API
//     response, and the LLM tool schema all read from a single
//     source of truth. Editing a description is one file.
//   - Every builder goes through buildFunctionTool, which handles
//     the description lookup, error wrapping, and functiontool.New
//     call in one place. Adding a new tool is one line + a handler.
//   - Workspace scoping is a closure captured at construction time.
//     Agents cannot target cross-workspace state because the
//     workspace ID never flows through tool arguments.
//
// main.go builds CommonToolDeps once per pod from the constructed
// storage.SpacesClient, SearXNG endpoint, and workspace ID, then
// calls BuildCommonTools and threads the result through
// runner.BuildOptions.LocalTools into every agent constructor.
package tools

import (
	"context"
	"fmt"

	adktool "google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/storage"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools/local"
)

// buildFunctionTool constructs an ADK tool.Tool for the given tool
// name by looking up its display metadata in the seed catalog and
// wiring the handler through functiontool.New. Every builder in this
// file goes through this helper, so description drift between the
// mobile API, the tools table, and the LLM schema is impossible —
// there is exactly one source of each string.
func buildFunctionTool[A, R any](toolName string, handler handlerFunc[A, R]) (adktool.Tool, error) {
	desc, ok := lookupToolDescription(toolName)
	if !ok {
		return nil, fmt.Errorf("tools: %q has no seed entry; add it to migrations/orchestrator/seed/tools.json", toolName)
	}
	adkHandler := func(tctx adktool.Context, args A) (R, error) {
		return handler(toolCtxOrBackground(tctx), args)
	}
	t, err := functiontool.New(
		functiontool.Config{Name: toolName, Description: desc},
		adkHandler,
	)
	if err != nil {
		return nil, fmt.Errorf("tools: construct %q: %w", toolName, err)
	}
	return t, nil
}

// lookupToolDescription returns the Description for the named tool
// from the seed catalog. Cached on first call via sync.Once — the
// seed is embedded at compile time and never changes at runtime, so
// we build a name→description map once and serve every lookup from
// memory.
func lookupToolDescription(name string) (string, bool) {
	toolDescOnce.Do(func() {
		catalog := DefaultCatalog()
		toolDescMap = make(map[string]string, len(catalog))
		for _, t := range catalog {
			toolDescMap[t.Name] = t.Description
		}
	})
	desc, ok := toolDescMap[name]
	return desc, ok
}

// toolCtxOrBackground returns context.Background() because the vendored
// ADK tool.Context interface does not expose the underlying invocation
// context via a public accessor.
//
// FUTURE: Thread per-turn ctx once the ADK exposes it on tool.Context
// (https://github.com/google/adk-go/issues/track). Currently every tool runs
// on background ctx, which means user disconnect does not cancel in-flight
// HTTP calls made from within tools.
func toolCtxOrBackground(tctx adktool.Context) context.Context {
	_ = tctx
	return context.Background()
}

// webFetchHandler calls local.WebFetch and reshapes the result into the
// LLM-facing webFetchResult struct. workspaceID / store / endpoint are
// captured from the closure; any new local tool follows the same pattern.
func webFetchHandler(ctx context.Context, args webFetchArgs) (webFetchResult, error) {
	body, err := local.WebFetch(ctx, local.WebFetchOptions{
		URL:            args.URL,
		MaxBytes:       args.MaxBytes,
		TimeoutSeconds: args.TimeoutSeconds,
	})
	if err != nil {
		return webFetchResult{URL: args.URL}, err
	}
	return webFetchResult{URL: args.URL, Body: body}, nil
}

func webSearchHandler(endpoint string) handlerFunc[webSearchArgs, webSearchResult] {
	return func(ctx context.Context, args webSearchArgs) (webSearchResult, error) {
		results, err := local.WebSearch(ctx, endpoint, local.WebSearchOptions{
			Query:      args.Query,
			MaxResults: args.MaxResults,
		})
		if err != nil {
			return webSearchResult{Query: args.Query}, err
		}
		return webSearchResult{Query: args.Query, Results: results}, nil
	}
}

func fileReadHandler(spaces *storage.SpacesClient, workspaceID string) handlerFunc[fileReadArgs, local.FileReadResult] {
	return func(ctx context.Context, args fileReadArgs) (local.FileReadResult, error) {
		return local.FileRead(ctx, spaces, workspaceID, local.FileReadOptions{Key: args.Key})
	}
}

func fileWriteHandler(spaces *storage.SpacesClient, workspaceID string) handlerFunc[fileWriteArgs, local.FileWriteResult] {
	return func(ctx context.Context, args fileWriteArgs) (local.FileWriteResult, error) {
		return local.FileWrite(ctx, spaces, workspaceID, local.FileWriteOptions{
			Key:         args.Key,
			Content:     args.Content,
			ContentType: args.ContentType,
		})
	}
}

// BuildCommonTools returns the full local tool slice the agents
// share. Every entry goes through buildFunctionTool with a Tool*
// constant from catalog.go, so renaming a tool is a one-line change
// and the description comes from the seed catalog. The returned
// slice mirrors the order the agents see in their tool list; file_*
// come last because they are optional.
func BuildCommonTools(deps CommonToolDeps) ([]adktool.Tool, error) {
	if deps.WorkspaceID == "" {
		return nil, fmt.Errorf("tools: BuildCommonTools requires a non-empty WorkspaceID")
	}
	if deps.SearXNGEndpoint == "" {
		return nil, fmt.Errorf("tools: BuildCommonTools requires a non-empty SearXNGEndpoint")
	}

	builders := []func() (adktool.Tool, error){
		func() (adktool.Tool, error) {
			return buildFunctionTool(ToolWebFetch, webFetchHandler)
		},
		func() (adktool.Tool, error) {
			return buildFunctionTool(ToolWebSearch, webSearchHandler(deps.SearXNGEndpoint))
		},
	}
	if deps.Spaces != nil {
		builders = append(builders,
			func() (adktool.Tool, error) {
				return buildFunctionTool(ToolFileRead, fileReadHandler(deps.Spaces, deps.WorkspaceID))
			},
			func() (adktool.Tool, error) {
				return buildFunctionTool(ToolFileWrite, fileWriteHandler(deps.Spaces, deps.WorkspaceID))
			},
		)
	}

	tools := make([]adktool.Tool, 0, len(builders))
	for _, b := range builders {
		t, err := b()
		if err != nil {
			return nil, err
		}
		tools = append(tools, t)
	}
	return tools, nil
}
