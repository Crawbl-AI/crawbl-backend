// Package agents constructs the concrete ADK llmagents that make up a
// Crawbl user swarm. Phase 1 ships three agents: Manager (root, LLM-driven
// router via SubAgents delegation), Wally (research / web), and Eve
// (scheduling / time). Every agent shares the same MCP toolset and the
// same model adapter — the differences are in the Instruction and the
// set of locally-bound Go tools.
//
// This file defines the tool adapters that wrap runtime-local Go
// functions (internal/agentruntime/tools/local/*) as ADK tool.Tool values
// via google.golang.org/adk/tool/functiontool. Agents pick the subset of
// these tools that their role allows by referencing the returned
// tool.Tool variables directly.
package agents

import (
	"context"
	"fmt"

	adktool "google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

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

// WebFetchResult is the typed output the tool returns to the LLM. Keeping
// URL + Body in one struct gives the LLM enough context to cite the
// source when it uses the content in its response.
type WebFetchResult struct {
	URL  string `json:"url" jsonschema:"the URL that was fetched"`
	Body string `json:"body" jsonschema:"response body text, possibly truncated"`
}

// NewWebFetchTool wraps local.WebFetch as an ADK tool.Tool ready to bind
// into an llmagent.Config.Tools slice. The name "web_fetch" matches the
// entry in the runtime's 37-tool catalog (internal/agentruntime/tools/
// catalog.go), so the mobile app's integration screen and the actual
// agent capability stay in sync.
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

// webFetchHandler is the tool.Context-flavored adapter that functiontool
// expects. It delegates to local.WebFetch for the actual HTTP call.
// The tool.Context carries cancellation via its Context() method, which
// we forward so the agent's run cancellation propagates to the HTTP
// request timeout logic.
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

// toolCtxOrBackground extracts a context.Context from an ADK tool.Context.
// The ADK tool.Context interface (google.golang.org/adk/tool/tool.go:55)
// provides cancellation via its underlying context; if the interface is
// ever nil (e.g. in isolated tests) we fall back to context.Background
// to avoid a nil-deref at the HTTP layer.
func toolCtxOrBackground(tctx adktool.Context) context.Context {
	if tctx == nil {
		return context.Background()
	}
	// The tool.Context interface embeds context-aware methods. The ADK
	// implementation carries an invocation context under the hood; the
	// ReadonlyContext form surfaces it via a Context-like accessor, but
	// the public interface in Phase 1 forces us to hand the agent's
	// context through other means. For Phase 1 we use Background — the
	// HTTP layer has its own timeout ceiling so cancellation propagation
	// is not load-bearing yet. US-AR-009 will wire the Converse stream
	// context through the runner so we have a real ctx here.
	return context.Background()
}
