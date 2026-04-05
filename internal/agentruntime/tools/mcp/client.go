package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
)

// Client is the crawbl-agent-runtime's bridge to the orchestrator's MCP
// server. It owns a single ClientSession for the lifetime of the runtime
// process and exposes a minimal Call(toolName, args) surface that
// US-AR-008's agent wiring can plug into llmagent.Config.Tools.
//
// The session is established once in New() and reused for every tool
// invocation. If the session breaks mid-run (network blip, orchestrator
// restart), the runtime currently returns the error up the stack and
// lets the caller decide whether to reconnect. US-AR-009 adds
// auto-reconnect once the Converse bidi stream is wired and we have
// real failure patterns to learn from.
//
// Concurrency: Call() is safe to invoke from multiple goroutines — the
// MCP SDK's ClientSession is itself concurrent-safe per upstream docs
// — but Close() must only be called once.
type Client struct {
	logger  *slog.Logger
	session *sdkmcp.ClientSession

	mu     sync.Mutex
	closed bool
}

// ErrOrchestratorToolError wraps a tool call that returned with
// CallToolResult.IsError=true. The runtime treats this as a structured
// tool failure rather than a transport error — agents see a typed
// error they can reason about.
var ErrOrchestratorToolError = errors.New("mcp: orchestrator tool returned an error")

// New connects to the orchestrator's MCP server at cfg.MCPEndpoint and
// returns a ready-to-use Client. The HTTP client is wired with an HMAC
// round-tripper so every outbound request carries a freshly-signed
// bearer token. Connection is synchronous; callers that need to bound
// startup time should wrap the call in their own context.WithTimeout.
//
// Caller is responsible for calling Close() on shutdown; the runtime's
// main.go does this in its signal handler.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MCPEndpoint == "" {
		return nil, errors.New("mcp: MCPEndpoint is required")
	}

	httpClient, err := newSignedHTTPClient(cfg.MCPSigningKey, cfg.UserID, cfg.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("mcp: build signed HTTP client: %w", err)
	}

	// StreamableClientTransport matches the server-side
	// sdkmcp.NewStreamableHTTPHandler used by
	// internal/orchestrator/mcp/server.go:46. Same SDK, compatible wire
	// format, no custom framing.
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:   cfg.MCPEndpoint,
		HTTPClient: httpClient,
		// The runtime only calls orchestrator tools in response to agent
		// requests; we don't need the persistent server-initiated SSE
		// stream. Disabling it avoids a long-running goroutine per runtime
		// process and simplifies shutdown ordering.
		DisableStandaloneSSE: true,
	}

	client := sdkmcp.NewClient(
		&sdkmcp.Implementation{
			Name:    "crawbl-agent-runtime",
			Version: "phase1",
		},
		nil,
	)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: connect to orchestrator at %s: %w", cfg.MCPEndpoint, err)
	}
	logger.Info("mcp client connected to orchestrator", "endpoint", cfg.MCPEndpoint)

	return &Client{
		logger:  logger,
		session: session,
	}, nil
}

// Call invokes an orchestrator-mediated tool by name and returns the
// tool's response text. Arguments are marshaled to JSON before being
// passed to the MCP SDK, which re-marshals them into the wire format —
// the double trip is unavoidable because the SDK's CallToolParams takes
// a map[string]any, not a pre-serialized payload.
//
// This is the entry point every orchestrator__* tool uses. The mapping
// from runtime tool name (e.g. "orchestrator__get_user_profile") to
// orchestrator MCP tool name is a 1:1 strip-prefix: "get_user_profile".
// The stripping happens here so callers can use the runtime catalog
// names directly.
func (c *Client) Call(ctx context.Context, runtimeToolName string, args any) (string, error) {
	if c == nil {
		return "", errors.New("mcp: Client is nil")
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return "", errors.New("mcp: Client is closed")
	}
	c.mu.Unlock()

	orchestratorToolName := stripOrchestratorPrefix(runtimeToolName)

	// Args may arrive as either a concrete Go struct, a map, or raw
	// json.RawMessage from the agent's tool call. Normalize to
	// map[string]any so sdkmcp.CallToolParams.Arguments is happy.
	var argMap map[string]any
	switch v := args.(type) {
	case nil:
		argMap = nil
	case map[string]any:
		argMap = v
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("mcp: marshal %s args: %w", runtimeToolName, err)
		}
		if len(raw) == 0 || string(raw) == "null" {
			argMap = nil
		} else {
			if err := json.Unmarshal(raw, &argMap); err != nil {
				return "", fmt.Errorf("mcp: unmarshal %s args into map: %w", runtimeToolName, err)
			}
		}
	}

	params := &sdkmcp.CallToolParams{
		Name:      orchestratorToolName,
		Arguments: argMap,
	}

	result, err := c.session.CallTool(ctx, params)
	if err != nil {
		return "", fmt.Errorf("mcp: call %s: %w", orchestratorToolName, err)
	}
	if result == nil {
		return "", fmt.Errorf("mcp: call %s: nil result", orchestratorToolName)
	}
	text := extractTextContent(result.Content)
	if result.IsError {
		return text, fmt.Errorf("%w: %s: %s", ErrOrchestratorToolError, orchestratorToolName, text)
	}
	return text, nil
}

// Close tears down the MCP session. Safe to call multiple times.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.session == nil {
		return nil
	}
	if err := c.session.Close(); err != nil {
		c.logger.Warn("mcp session close returned error", "error", err)
		return err
	}
	return nil
}

// stripOrchestratorPrefix converts a runtime catalog tool name to the
// orchestrator-side MCP tool name. The runtime catalog prefixes every
// orchestrator-mediated tool with "orchestrator__" so the mobile app's
// integration screen can group them; the orchestrator-side handler
// expects the bare name.
func stripOrchestratorPrefix(runtimeToolName string) string {
	const prefix = "orchestrator__"
	if len(runtimeToolName) > len(prefix) && runtimeToolName[:len(prefix)] == prefix {
		return runtimeToolName[len(prefix):]
	}
	return runtimeToolName
}

// extractTextContent pulls a single display string out of an MCP
// CallToolResult. The MCP spec allows rich content blocks (text, image,
// resource) but for Phase 1 we collapse everything to the concatenated
// text of any TextContent blocks. Image / resource blocks fall through
// as empty string — an agent that wants the structured shape can graduate
// to the richer surface later.
func extractTextContent(content []sdkmcp.Content) string {
	if len(content) == 0 {
		return ""
	}
	var out []byte
	for _, block := range content {
		if tc, ok := block.(*sdkmcp.TextContent); ok {
			out = append(out, []byte(tc.Text)...)
		}
	}
	return string(out)
}
