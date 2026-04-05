package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

// WorkspaceBlueprint describes the agent graph a runtime should build
// for a given workspace. It is the wire shape the orchestrator's
// GET /v1/internal/agents?workspace_id=<id> endpoint returns, decoded
// directly into this struct.
//
// The type is deliberately flat — one entry per agent — so the wire
// shape stays stable and matches the orchestrator handler at
// internal/orchestrator/server/handler/internal_runtime.go.
type WorkspaceBlueprint struct {
	// WorkspaceID is the Crawbl workspace this blueprint describes.
	WorkspaceID string `json:"workspace_id"`
	// Agents is the full list of agents the runtime should construct
	// for this workspace. Sourced from the orchestrator's agents +
	// agent_prompts + agent_settings rows.
	Agents []AgentBlueprint `json:"agents"`
}

// AgentBlueprint describes a single agent's configuration. Field
// names mirror the orchestrator's agents / agent_settings /
// agent_prompts columns so the HTTP handler can marshal rows directly
// without translation.
type AgentBlueprint struct {
	// Slug is the stable routing identifier used by the orchestrator
	// to target a specific agent via ConverseRequest.AgentID.
	Slug string `json:"slug"`
	// Role is either "manager" or "sub-agent".
	Role string `json:"role"`
	// SystemPrompt is the agent's instruction text — what
	// llmagent.Config.Instruction carries.
	SystemPrompt string `json:"system_prompt"`
	// Description is the one-line capability summary surfaced in mobile
	// and used by Manager's routing LLM to pick the right sub-agent.
	Description string `json:"description"`
	// AllowedTools is the subset of tool names (from the runtime tool
	// catalog) this agent is allowed to invoke. Empty means "all".
	AllowedTools []string `json:"allowed_tools"`
	// Model optionally overrides the workspace-level default LLM for
	// this agent. Empty means use the workspace default.
	Model string `json:"model"`
}

// agentBySlug returns the matching AgentBlueprint and true if the slug
// exists in the blueprint, or a zero value and false otherwise.
func (b *WorkspaceBlueprint) agentBySlug(slug string) (AgentBlueprint, bool) {
	if b == nil {
		return AgentBlueprint{}, false
	}
	for _, a := range b.Agents {
		if a.Slug == slug {
			return a, true
		}
	}
	return AgentBlueprint{}, false
}

// FetchBlueprint fetches the WorkspaceBlueprint from the orchestrator's
// internal endpoint GET /v1/internal/agents?workspace_id=<id>, signed
// with an HMAC bearer token derived from (userID, workspaceID) using
// the same scheme the MCP bridge uses (internal/pkg/hmac).
//
// A fetch failure is fatal: the runtime cannot boot a meaningful
// swarm without the blueprint, and silently booting a stale or
// hardcoded graph would make the failure invisible to operators.
// main.go propagates the error and exits with a non-zero status so
// Kubernetes restarts the pod, at which point the orchestrator must
// be reachable before the runtime serves traffic.
func FetchBlueprint(ctx context.Context, cfg config.Config, logger *slog.Logger) (*WorkspaceBlueprint, error) {
	if logger == nil {
		logger = slog.Default()
	}

	endpoint, err := resolveBlueprintEndpoint(cfg)
	if err != nil {
		return nil, fmt.Errorf("resolve blueprint endpoint: %w", err)
	}

	bp, err := fetchBlueprintHTTP(ctx, endpoint, cfg)
	if err != nil {
		logger.Error("runner: blueprint fetch failed",
			"workspace_id", cfg.WorkspaceID,
			"endpoint", endpoint,
			"error", err,
		)
		return nil, err
	}

	logger.Info("runner: workspace blueprint fetched from orchestrator",
		"workspace_id", bp.WorkspaceID,
		"agent_count", len(bp.Agents),
		"endpoint", endpoint,
	)
	return bp, nil
}

// resolveBlueprintEndpoint builds the full orchestrator URL for the
// GET /v1/internal/agents call. The orchestrator's MCP endpoint
// already carries the host + /mcp/v1 path; we strip the MCP suffix
// and append /v1/internal/agents?workspace_id=<id>. This keeps the
// runtime config minimal — one endpoint env var covers both MCP and
// internal routes.
func resolveBlueprintEndpoint(cfg config.Config) (string, error) {
	if strings.TrimSpace(cfg.MCPEndpoint) == "" {
		return "", errors.New("MCPEndpoint is empty")
	}
	u, err := url.Parse(cfg.MCPEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse MCPEndpoint: %w", err)
	}
	// Drop the MCP path segment (typically "/mcp/v1") and install the
	// internal-agents path.
	u.Path = "/v1/internal/agents"
	q := u.Query()
	q.Set("workspace_id", cfg.WorkspaceID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// fetchBlueprintHTTP performs the authenticated GET. Separated from
// FetchBlueprint so the retry and logging policy has a clean boundary:
// FetchBlueprint handles policy (log, propagate); fetchBlueprintHTTP
// handles mechanics (sign, send, decode, validate).
func fetchBlueprintHTTP(ctx context.Context, endpoint string, cfg config.Config) (*WorkspaceBlueprint, error) {
	if cfg.MCPSigningKey == "" {
		return nil, errors.New("MCPSigningKey is empty")
	}
	if cfg.UserID == "" || cfg.WorkspaceID == "" {
		return nil, errors.New("UserID and WorkspaceID are required for HMAC")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	token := crawblhmac.GenerateToken(cfg.MCPSigningKey, cfg.UserID, cfg.WorkspaceID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "crawbl-agent-runtime (+blueprint-fetch)")

	client := &http.Client{
		Timeout: cfg.Startup.BlueprintFetchTimeout,
	}
	if client.Timeout <= 0 {
		client.Timeout = 15 * time.Second
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("orchestrator returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var bp WorkspaceBlueprint
	if err := json.Unmarshal(body, &bp); err != nil {
		return nil, fmt.Errorf("decode blueprint JSON: %w", err)
	}
	if bp.WorkspaceID == "" {
		return nil, errors.New("orchestrator returned blueprint with empty workspace_id")
	}
	if len(bp.Agents) == 0 {
		return nil, errors.New("orchestrator returned blueprint with zero agents")
	}
	return &bp, nil
}
