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

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/agents"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

// WorkspaceBlueprint describes the agent graph a runtime should build
// for a given workspace. It's the wire shape the orchestrator's future
// GET /v1/internal/agents?workspace_id=<id> endpoint returns, decoded
// directly into this struct.
//
// Phase 1 scope: the type exists and the runtime fetches it at startup,
// but the orchestrator endpoint that returns it does NOT exist yet.
// FetchBlueprint below returns a hardcoded default that matches the
// Phase 1 Manager+Wally+Eve graph, plus logs a warning so operators
// know the real fetch path is not wired.
//
// Phase 2 scope: add the orchestrator handler (HMAC-authed, not on the
// public HTTPRoute), wire AgentRepo + AgentSettingsRepo + AgentPromptsRepo
// through it, and replace the body of FetchBlueprint with a real HTTP
// GET using the same internal/pkg/hmac signing scheme the MCP bridge
// already uses.
//
// The type is deliberately flat — one entry per agent — so the wire
// shape stays stable across Phase 2's swap from hardcoded to HTTP.
type WorkspaceBlueprint struct {
	// WorkspaceID is the Crawbl workspace this blueprint describes.
	WorkspaceID string `json:"workspace_id"`
	// Agents is the full list of agents the runtime should construct
	// for this workspace. In Phase 1 this is always three entries
	// (Manager, Wally, Eve); Phase 2+ drives it from Postgres.
	Agents []AgentBlueprint `json:"agents"`
}

// AgentBlueprint describes a single agent's configuration as stored in
// the orchestrator's agents + agent_settings + agent_prompts tables.
// Field names mirror those three tables so the Phase 2 HTTP handler
// can marshal rows directly without translation.
type AgentBlueprint struct {
	// Slug is the stable routing identifier used by the orchestrator
	// to target a specific agent via ConverseRequest.AgentID.
	Slug string `json:"slug"`
	// Role is either "manager" or "sub-agent" (matching the existing
	// orchestrator DefaultAgentBlueprint convention in
	// internal/orchestrator/types.go:700+).
	Role string `json:"role"`
	// SystemPrompt is the agent's instruction text (what llmagent.Config.Instruction carries).
	SystemPrompt string `json:"system_prompt"`
	// Description is the one-line capability summary surfaced in mobile
	// and used by Manager's routing LLM to pick the right sub-agent.
	Description string `json:"description"`
	// AllowedTools is the subset of tool names (from the 37-tool
	// catalog) this agent is allowed to invoke. Empty means "all".
	AllowedTools []string `json:"allowed_tools"`
	// Model optionally overrides the workspace-level default LLM for
	// this agent. Empty means use the workspace default.
	Model string `json:"model"`
}

// FetchBlueprint fetches the WorkspaceBlueprint from the orchestrator's
// internal endpoint GET /v1/internal/agents?workspace_id=<id>, signed
// with an HMAC bearer token derived from (userID, workspaceID) using
// the same scheme the MCP bridge uses (internal/pkg/hmac).
//
// On transport or decode failure we log a warning and fall back to
// DefaultBlueprint so the runtime can still boot a usable swarm. This
// matters during cold-start when the orchestrator may briefly be
// unreachable; an offline runtime is worse than a runtime running
// last-known-good defaults. Operators see the fallback via the warning
// log and can re-provision if needed.
//
// The orchestrator endpoint URL is derived from cfg.OrchestratorHTTPEndpoint
// when set, otherwise falls back to the same host as MCPEndpoint with
// the path replaced.
func FetchBlueprint(ctx context.Context, cfg config.Config, logger *slog.Logger) (*WorkspaceBlueprint, error) {
	if logger == nil {
		logger = slog.Default()
	}

	endpoint, err := resolveBlueprintEndpoint(cfg)
	if err != nil {
		logger.Warn("runner: cannot resolve blueprint endpoint, using hardcoded default",
			"workspace_id", cfg.WorkspaceID,
			"error", err,
		)
		return DefaultBlueprint(cfg.WorkspaceID), nil
	}

	bp, err := fetchBlueprintHTTP(ctx, endpoint, cfg, logger)
	if err != nil {
		logger.Warn("runner: blueprint fetch failed, using hardcoded default",
			"workspace_id", cfg.WorkspaceID,
			"endpoint", endpoint,
			"error", err,
		)
		return DefaultBlueprint(cfg.WorkspaceID), nil
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
// already carries the host + /mcp/v1 path; we strip the MCP suffix and
// append /v1/internal/agents?workspace_id=<id>. This keeps the runtime
// config minimal — one endpoint env var covers both MCP and internal
// routes.
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
// FetchBlueprint so the fallback-on-error logic has a clean boundary:
// FetchBlueprint handles policy (fall back to default on error);
// fetchBlueprintHTTP handles mechanics (sign, send, decode).
func fetchBlueprintHTTP(ctx context.Context, endpoint string, cfg config.Config, logger *slog.Logger) (*WorkspaceBlueprint, error) {
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
	req.Header.Set("User-Agent", "crawbl-agent-runtime/phase2 (+blueprint-fetch)")

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
	_ = logger // kept for future debug logging
	return &bp, nil
}

// DefaultBlueprint returns the hardcoded Phase 1 blueprint that matches
// the Manager+Wally+Eve graph BuildGraph constructs. It is exported so
// Phase 2's fallback-on-fetch-error path can reference it, and so tests
// (when we re-introduce them post-Phase-1) can check the default shape
// without touching the orchestrator.
//
// The system prompts here are intentionally NOT the full prompts used
// by agents/manager.go, agents/wally.go, and agents/eve.go — those
// live in their respective agent files and stay the source of truth
// for Phase 1. This blueprint is a metadata snapshot, not an
// instruction dispatcher. Phase 2's dynamic construction flow will
// consume SystemPrompt from the blueprint and pass it into
// llmagent.Config.Instruction, at which point the duplication
// disappears.
func DefaultBlueprint(workspaceID string) *WorkspaceBlueprint {
	return &WorkspaceBlueprint{
		WorkspaceID: workspaceID,
		Agents: []AgentBlueprint{
			{
				Slug:         agents.ManagerName,
				Role:         "manager",
				SystemPrompt: "Crawbl swarm coordinator. Routes user messages to the right specialist (wally for research, eve for scheduling) or answers simple requests directly.",
				Description:  "Root router agent; delegates to sub-agents or answers directly.",
				AllowedTools: nil, // Manager can use any tool (orchestrator MCP + whatever sub-agents have).
				Model:        "", // Use workspace default.
			},
			{
				Slug:         agents.WallyName,
				Role:         "sub-agent",
				SystemPrompt: "Research specialist. Fetches web pages and extracts information for the user.",
				Description:  "Web research agent; uses web_fetch to read pages and extract content.",
				AllowedTools: []string{"web_fetch", "web_search_tool"},
				Model:        "",
			},
			{
				Slug:         agents.EveName,
				Role:         "sub-agent",
				SystemPrompt: "Scheduling and time specialist. Handles time zones, date math, and scheduling conventions.",
				Description:  "Scheduling agent; handles time zones and date math.",
				AllowedTools: nil,
				Model:        "",
			},
		},
	}
}
