// Package agents constructs the concrete ADK llmagents that make up a
// Crawbl user swarm. The three agents — Manager (root, LLM-driven
// router via SubAgents delegation), Wally (research), and Eve
// (scheduling) — share the same MCP toolset and model adapter; the
// differences are in the instruction text (pulled from the workspace
// blueprint) and the local tool set each agent is allowed to use.
//
// This file defines the tool adapters that wrap runtime-local Go
// functions (internal/agentruntime/tools/local/*) as ADK tool.Tool
// values via google.golang.org/adk/tool/functiontool. The runner
// constructs the shared tool slice once per pod and hands it to every
// agent constructor, so all three agents see the same local tools.
package agents

import (
	"context"
	"fmt"

	adktool "google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools/local"
)

// WebFetchArgs is the typed input schema the LLM sees for the web_fetch
// tool. The jsonschema tags become the tool's OpenAPI-style argument
// documentation that the LLM uses to decide what to pass in.
type WebFetchArgs struct {
	URL            string `json:"url" jsonschema:"HTTP(S) URL to fetch"`
	MaxBytes       int64  `json:"max_bytes,omitempty" jsonschema:"optional cap on response body bytes (default 2 MiB)"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"optional request timeout in seconds (default 10)"`
}

// WebFetchResult is the typed output the tool returns to the LLM.
// Keeping URL + Body in one struct gives the LLM enough context to
// cite the source when it uses the content in its response.
type WebFetchResult struct {
	URL  string `json:"url" jsonschema:"the URL that was fetched"`
	Body string `json:"body" jsonschema:"response body text, possibly truncated"`
}

// NewWebFetchTool wraps local.WebFetch as an ADK tool.Tool ready to
// bind into an llmagent.Config.Tools slice. The name "web_fetch"
// matches the entry in the runtime's tool catalog, so the mobile
// app's integration screen and the actual agent capability stay in
// sync.
func NewWebFetchTool() (adktool.Tool, error) {
	t, err := functiontool.New(
		functiontool.Config{
			Name:        "web_fetch",
			Description: "Read and extract content from any webpage URL. Returns the raw text or HTML body.",
		},
		webFetchHandler,
	)
	if err != nil {
		return nil, fmt.Errorf("agents: construct web_fetch tool: %w", err)
	}
	return t, nil
}

func webFetchHandler(tctx adktool.Context, args WebFetchArgs) (WebFetchResult, error) {
	ctx := toolCtxOrBackground(tctx)
	body, err := local.WebFetch(ctx, local.WebFetchOptions{
		URL:            args.URL,
		MaxBytes:       args.MaxBytes,
		TimeoutSeconds: args.TimeoutSeconds,
	})
	if err != nil {
		return WebFetchResult{URL: args.URL}, err
	}
	return WebFetchResult{URL: args.URL, Body: body}, nil
}

// --- web_search_tool ------------------------------------------------

// WebSearchArgs is the LLM-facing schema for web_search_tool. Keep the
// description tight — research agents decide whether to search based
// on this blurb.
type WebSearchArgs struct {
	Query      string `json:"query" jsonschema:"free-text search query; required"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"optional cap on returned results (default 5, max 15)"`
}

// WebSearchResponse is the typed output. Wrapping the slice in a
// named struct gives the LLM a stable field to iterate over and a
// place to attach metadata in future iterations.
type WebSearchResponse struct {
	Query   string                  `json:"query"`
	Results []local.WebSearchResult `json:"results"`
}

// NewWebSearchTool wraps local.WebSearch as an ADK tool.Tool. The
// SearXNG endpoint is captured at construction time so the handler
// has no hidden dependencies on globals or environment variables.
// The tool is constructed once per pod in main.go and shared across
// every agent that lists web_search_tool in its allowed tools.
func NewWebSearchTool(searxngEndpoint string) (adktool.Tool, error) {
	if searxngEndpoint == "" {
		return nil, fmt.Errorf("agents: web_search_tool requires a non-empty SearXNG endpoint")
	}
	handler := func(tctx adktool.Context, args WebSearchArgs) (WebSearchResponse, error) {
		results, err := local.WebSearch(toolCtxOrBackground(tctx), searxngEndpoint, local.WebSearchOptions{
			Query:      args.Query,
			MaxResults: args.MaxResults,
		})
		if err != nil {
			return WebSearchResponse{Query: args.Query}, err
		}
		return WebSearchResponse{Query: args.Query, Results: results}, nil
	}
	t, err := functiontool.New(
		functiontool.Config{
			Name:        "web_search_tool",
			Description: "Search the web and return a ranked list of results (title, URL, snippet). Use this when the user wants information from the live internet but has not given you a specific URL to fetch.",
		},
		handler,
	)
	if err != nil {
		return nil, fmt.Errorf("agents: construct web_search_tool: %w", err)
	}
	return t, nil
}

// --- memory_store ---------------------------------------------------

// MemoryStoreArgs is the LLM-facing schema for memory_store.
type MemoryStoreArgs struct {
	Key      string `json:"key" jsonschema:"stable identifier to recall this memory by; required"`
	Content  string `json:"content" jsonschema:"the memory text to persist; required"`
	Category string `json:"category,omitempty" jsonschema:"optional topic tag so memory_recall can filter by category"`
}

// MemoryStoreResult is the persisted entry echoed back so the LLM
// can confirm the write succeeded and see the server-assigned
// timestamps.
type MemoryStoreResult struct {
	Key       string `json:"key"`
	Content   string `json:"content"`
	Category  string `json:"category,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// NewMemoryStoreTool wraps local.MemoryStore. The workspace ID is
// captured at construction time from the runtime's own config so
// agents cannot write memories into another workspace, even if they
// try to pass a different workspace_id in their tool arguments.
func NewMemoryStoreTool(store memory.Store, workspaceID string) (adktool.Tool, error) {
	if store == nil {
		return nil, fmt.Errorf("agents: memory_store requires a non-nil memory.Store")
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("agents: memory_store requires a non-empty workspace id")
	}
	handler := func(tctx adktool.Context, args MemoryStoreArgs) (MemoryStoreResult, error) {
		entry, err := local.MemoryStore(toolCtxOrBackground(tctx), store, workspaceID, local.MemoryStoreOptions{
			Key:      args.Key,
			Content:  args.Content,
			Category: args.Category,
		})
		if err != nil {
			return MemoryStoreResult{}, err
		}
		return MemoryStoreResult{
			Key:       entry.Key,
			Content:   entry.Content,
			Category:  entry.Category,
			CreatedAt: entry.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			UpdatedAt: entry.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}, nil
	}
	t, err := functiontool.New(
		functiontool.Config{
			Name:        "memory_store",
			Description: "Save a durable note about the user, their preferences, or any fact the agent should remember across future conversations. Pair with memory_recall to retrieve what you stored.",
		},
		handler,
	)
	if err != nil {
		return nil, fmt.Errorf("agents: construct memory_store tool: %w", err)
	}
	return t, nil
}

// --- memory_recall --------------------------------------------------

// MemoryRecallArgs is the LLM-facing schema for memory_recall.
type MemoryRecallArgs struct {
	Category string `json:"category,omitempty" jsonschema:"optional category filter; empty returns all entries"`
	Limit    int    `json:"limit,omitempty" jsonschema:"optional cap on entries returned (default 100)"`
	Offset   int    `json:"offset,omitempty" jsonschema:"optional pagination offset"`
}

// MemoryRecallEntry mirrors memory.Entry on the wire in a shape the
// LLM can cite directly.
type MemoryRecallEntry struct {
	Key       string `json:"key"`
	Content   string `json:"content"`
	Category  string `json:"category,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// MemoryRecallResponse is the typed response. Wrapping the list in a
// struct keeps the door open to attaching totals / next_offset later.
type MemoryRecallResponse struct {
	Entries []MemoryRecallEntry `json:"entries"`
}

// NewMemoryRecallTool wraps local.MemoryRecall. workspaceID is
// captured at construction time for the same reason memory_store
// captures it — agents cannot target another workspace's memories.
func NewMemoryRecallTool(store memory.Store, workspaceID string) (adktool.Tool, error) {
	if store == nil {
		return nil, fmt.Errorf("agents: memory_recall requires a non-nil memory.Store")
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("agents: memory_recall requires a non-empty workspace id")
	}
	handler := func(tctx adktool.Context, args MemoryRecallArgs) (MemoryRecallResponse, error) {
		entries, err := local.MemoryRecall(toolCtxOrBackground(tctx), store, workspaceID, local.MemoryRecallOptions{
			Category: args.Category,
			Limit:    args.Limit,
			Offset:   args.Offset,
		})
		if err != nil {
			return MemoryRecallResponse{}, err
		}
		out := make([]MemoryRecallEntry, 0, len(entries))
		for _, e := range entries {
			out = append(out, MemoryRecallEntry{
				Key:       e.Key,
				Content:   e.Content,
				Category:  e.Category,
				UpdatedAt: e.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			})
		}
		return MemoryRecallResponse{Entries: out}, nil
	}
	t, err := functiontool.New(
		functiontool.Config{
			Name:        "memory_recall",
			Description: "Retrieve previously saved memories for the current user, optionally filtered by category. Use this before answering personal or preference-sensitive questions.",
		},
		handler,
	)
	if err != nil {
		return nil, fmt.Errorf("agents: construct memory_recall tool: %w", err)
	}
	return t, nil
}

// --- memory_forget --------------------------------------------------

// MemoryForgetArgs is the LLM-facing schema for memory_forget.
type MemoryForgetArgs struct {
	Key string `json:"key" jsonschema:"key of the memory entry to delete; required"`
}

// MemoryForgetResult echoes the deleted key so the LLM has something
// structured to confirm in its response.
type MemoryForgetResult struct {
	Key     string `json:"key"`
	Deleted bool   `json:"deleted"`
}

// NewMemoryForgetTool wraps local.MemoryForget.
func NewMemoryForgetTool(store memory.Store, workspaceID string) (adktool.Tool, error) {
	if store == nil {
		return nil, fmt.Errorf("agents: memory_forget requires a non-nil memory.Store")
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("agents: memory_forget requires a non-empty workspace id")
	}
	handler := func(tctx adktool.Context, args MemoryForgetArgs) (MemoryForgetResult, error) {
		if err := local.MemoryForget(toolCtxOrBackground(tctx), store, workspaceID, local.MemoryForgetOptions{Key: args.Key}); err != nil {
			return MemoryForgetResult{Key: args.Key, Deleted: false}, err
		}
		return MemoryForgetResult{Key: args.Key, Deleted: true}, nil
	}
	t, err := functiontool.New(
		functiontool.Config{
			Name:        "memory_forget",
			Description: "Delete a previously saved memory by key. Use this when the user asks to forget something or withdraws a preference.",
		},
		handler,
	)
	if err != nil {
		return nil, fmt.Errorf("agents: construct memory_forget tool: %w", err)
	}
	return t, nil
}

// --- BuildCommonTools -----------------------------------------------

// CommonToolDeps carries the backend handles every local tool needs.
// main.go builds this once per pod from config + constructed stores
// and hands it to runner.New, which in turn calls BuildCommonTools
// below to produce the slice that every agent shares.
type CommonToolDeps struct {
	// MemStore is the durable memory.Store (Postgres-backed in
	// production). Captured at construction time by memory_store /
	// memory_recall / memory_forget.
	MemStore memory.Store
	// WorkspaceID is the Crawbl workspace this pod serves. The
	// memory tools scope every read and write to this workspace.
	WorkspaceID string
	// SearXNGEndpoint is the base URL of the internal meta-search
	// instance (see crawbl-argocd-apps/components/searxng/). Captured
	// at construction time by web_search_tool.
	SearXNGEndpoint string
}

// BuildCommonTools returns the full local tool slice the agents
// share: web_fetch, web_search_tool, memory_store, memory_recall,
// memory_forget. Any constructor failure aborts startup — there is
// no fallback path that silently drops a tool.
func BuildCommonTools(deps CommonToolDeps) ([]adktool.Tool, error) {
	webFetch, err := NewWebFetchTool()
	if err != nil {
		return nil, err
	}
	webSearch, err := NewWebSearchTool(deps.SearXNGEndpoint)
	if err != nil {
		return nil, err
	}
	memStore, err := NewMemoryStoreTool(deps.MemStore, deps.WorkspaceID)
	if err != nil {
		return nil, err
	}
	memRecall, err := NewMemoryRecallTool(deps.MemStore, deps.WorkspaceID)
	if err != nil {
		return nil, err
	}
	memForget, err := NewMemoryForgetTool(deps.MemStore, deps.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return []adktool.Tool{webFetch, webSearch, memStore, memRecall, memForget}, nil
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
