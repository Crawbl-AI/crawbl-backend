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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	token := crawblhmac.GenerateToken(cfg.MCPSigningKey, cfg.UserID, cfg.WorkspaceID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "crawbl-agent-runtime (+blueprint-fetch)")

	const (
		defaultBlueprintFetchTimeout = 15 * time.Second
		blueprintErrorStatusThresh   = 400
	)

	client := &http.Client{
		Timeout: cfg.Startup.BlueprintFetchTimeout,
	}
	if client.Timeout <= 0 {
		client.Timeout = defaultBlueprintFetchTimeout
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= blueprintErrorStatusThresh {
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
