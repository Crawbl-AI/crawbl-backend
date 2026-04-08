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

// --- Argument + result types (LLM-facing schemas) -------------------
//
// Every builder advertises its input/output shape to the LLM through
// these typed structs. Field tags carry both the JSON wire name and
// the jsonschema description functiontool turns into the tool's
// argument documentation.
//
// Deliberately kept close to the local.*Options shapes so the
// builders stay one-line adapters; the only reason these types exist
// instead of reusing local.*Options directly is the jsonschema tag
// layer, which would otherwise leak LLM-ergonomics concerns into the
// storage/memory packages.

type webFetchArgs struct {
	URL            string `json:"url" jsonschema:"HTTP(S) URL to fetch; required"`
	MaxBytes       int64  `json:"max_bytes,omitempty" jsonschema:"optional cap on response body bytes (default 2 MiB)"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"optional request timeout in seconds (default 10)"`
}

type webFetchResult struct {
	URL  string `json:"url"`
	Body string `json:"body"`
}

type webSearchArgs struct {
	Query      string `json:"query" jsonschema:"free-text search query; required"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"optional cap on returned results (default 5, max 15)"`
}

type webSearchResult struct {
	Query   string                  `json:"query"`
	Results []local.WebSearchResult `json:"results"`
}

type fileReadArgs struct {
	Key string `json:"key" jsonschema:"object key under the workspace, e.g. uploads/trip.md; required"`
}

type fileWriteArgs struct {
	Key         string `json:"key" jsonschema:"object key under the workspace, e.g. drafts/email.md; required"`
	Content     string `json:"content" jsonschema:"file body; required"`
	ContentType string `json:"content_type,omitempty" jsonschema:"optional MIME type (default text/plain)"`
}

// --- Generic builder ------------------------------------------------

// handlerFunc is the shape every local-tool adapter implements: take
// a context + typed args, return typed result + error. Each builder
// below is one of these wrapped through buildFunctionTool.
type handlerFunc[A, R any] func(ctx context.Context, args A) (R, error)

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
// from the seed catalog. Cached on first call — the seed is embedded
// at compile time and never changes at runtime.
func lookupToolDescription(name string) (string, bool) {
	for _, t := range DefaultCatalog() {
		if t.Name == name {
			return t.Description, true
		}
	}
	return "", false
}

// toolCtxOrBackground extracts a context.Context from an ADK
// tool.Context. The vendored ADK tool.Context interface does not
// expose the underlying invocation context via a public accessor, so
// for now we fall back to context.Background. Tool handlers apply
// their own timeouts (HTTP client, SQL query timeouts) so losing
// per-turn cancellation here is a bounded liability.
func toolCtxOrBackground(tctx adktool.Context) context.Context {
	if tctx == nil {
		return context.Background()
	}
	return context.Background()
}

// --- Per-tool handlers ----------------------------------------------
//
// Each handler is a three-line adapter: call the local package's
// implementation with workspaceID / store / endpoint captured from
// the closure, reshape the return into the LLM-facing result struct.
// Any new local tool follows the same pattern.

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

// --- BuildCommonTools -----------------------------------------------

// CommonToolDeps carries the backend handles every local tool needs.
// main.go builds this once per pod from config + constructed stores
// and hands it to runner.BuildOptions.LocalTools via BuildCommonTools.
type CommonToolDeps struct {
	// WorkspaceID is the Crawbl workspace this pod serves. The
	// file tools scope every read and write to this workspace via
	// closure capture.
	WorkspaceID string
	// SearXNGEndpoint is the base URL of the internal meta-search
	// instance. Captured at construction time by web_search_tool.
	SearXNGEndpoint string
	// Spaces is the DigitalOcean Spaces client that backs the
	// file_read / file_write tools. May be nil when storage is not
	// configured (local dev) — BuildCommonTools skips the file tools
	// in that case rather than returning an error, so the rest of
	// the tool set stays available.
	Spaces *storage.SpacesClient
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
