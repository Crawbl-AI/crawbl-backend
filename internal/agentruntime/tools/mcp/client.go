package mcp

import (
	"errors"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	adktool "google.golang.org/adk/tool"
	adkmcptoolset "google.golang.org/adk/tool/mcptoolset"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
)

// Toolset returns an ADK tool.Toolset that exposes every orchestrator-
// mediated tool as a typed ADK tool.Tool. It plugs directly into
// llmagent.Config.Toolsets so the LLM sees each tool's real name, input
// schema, and output schema as published by the orchestrator's MCP server,
// and calls them with typed arguments that ADK marshals into the wire
// format automatically.
//
// Under the hood this uses google.golang.org/adk/tool/mcptoolset, which:
//
//   - Calls ListTools on the orchestrator's MCP server to discover the
//     full tool set dynamically. Adding a tool on the orchestrator side
//     means the runtime picks it up on next reconnect — no runtime code
//     changes required.
//   - Converts each MCP Tool into an adk tool.Tool with the correct
//     jsonschema input/output types, so the LLM's tool-calling format
//     works unchanged and results come back as structured data, not
//     strings we have to parse.
//   - Handles connection refresh automatically on ErrConnectionClosed /
//     ErrSessionMissing / io.EOF. If the orchestrator restarts mid-run,
//     the next tool call reconnects transparently.
//
// This replaces an earlier hand-rolled client.Call(string, any) (string,
// error) shape that forced callers to JSON-round-trip args and re-parse
// text responses. The ADK-native path is strictly better on every axis:
// types, schema validation, reconnect, structured output.
//
// The caller must call Close on the returned Closer when the runtime
// shuts down to release the underlying MCP session.
func Toolset(cfg config.Config) (adktool.Toolset, Closer, error) {
	if cfg.MCPEndpoint == "" {
		return nil, nopCloser{}, errors.New("mcp: MCPEndpoint is required")
	}
	httpClient, err := newSignedHTTPClient(cfg.MCPSigningKey, cfg.UserID, cfg.WorkspaceID)
	if err != nil {
		return nil, nopCloser{}, fmt.Errorf("mcp: build signed HTTP client: %w", err)
	}

	// StreamableClientTransport matches the server-side handler used at
	// internal/orchestrator/mcp/server.go:46 (sdkmcp.NewStreamableHTTPHandler).
	// Same SDK version on both sides, so the wire format is guaranteed.
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:   cfg.MCPEndpoint,
		HTTPClient: httpClient,
		// The runtime only reacts to agent-initiated tool calls; we don't
		// need the server-initiated SSE notification stream. Disabling it
		// saves a goroutine per runtime process and simplifies shutdown.
		DisableStandaloneSSE: true,
	}

	ts, err := adkmcptoolset.New(adkmcptoolset.Config{
		Transport: transport,
	})
	if err != nil {
		return nil, nopCloser{}, fmt.Errorf("mcp: construct adk toolset: %w", err)
	}
	// The toolset's underlying client holds the MCP session. Closing it
	// during Shutdown tears the session down cleanly. We don't have a
	// direct handle to the session here because mcptoolset wraps it — the
	// nopCloser below is fine for Phase 1 because ADK's connectionRefresher
	// releases the session when the transport is GC'd; Phase 2 can upgrade
	// to an explicit Close if ADK exposes one.
	return ts, nopCloser{}, nil
}

// Closer is the shutdown hook returned alongside a Toolset. Phase 1
// implementation is a no-op because ADK mcptoolset does not expose an
// explicit session-close API — the session is torn down when the
// transport goes out of scope. This interface exists so US-AR-008's
// agent wiring and main.go's Shutdown can treat the MCP bridge like
// any other owned resource without caring about the underlying impl.
type Closer interface {
	Close() error
}

// nopCloser is the Phase 1 no-op Closer returned by Toolset. This is
// NOT interchangeable with io.NopCloser — io.NopCloser wraps an
// io.Reader and returns an io.ReadCloser; our Closer interface is a
// local one whose Close returns an error and takes no reader, so we
// need a dedicated zero-value type here.
type nopCloser struct{}

// Close is a no-op.
func (nopCloser) Close() error { return nil }
