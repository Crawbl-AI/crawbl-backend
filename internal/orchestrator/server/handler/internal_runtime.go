package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

// GetWorkspaceBlueprint returns the agent blueprints for a workspace, as
// consumed by the new crawbl-agent-runtime (internal/agentruntime/runner/
// blueprint.go) at pod startup. This endpoint replaces the Phase 1 stub
// FetchBlueprint path that returned hardcoded agents.
//
// Route: GET /v1/internal/agents?workspace_id=<id>
//
// Auth: HMAC bearer (same scheme as the orchestrator MCP server at
// internal/orchestrator/mcp/server.go:66). The runtime pod signs a token
// with CRAWBL_MCP_SIGNING_KEY encoding (userID, workspaceID); this handler
// validates the signature and uses the extracted IDs to enforce workspace
// ownership through the existing ChatService.ListAgents authz path.
//
// The endpoint is registered under /v1/internal which must NOT be exposed
// on the public Envoy HTTPRoute — it is only reachable from pods inside
// the cluster network.
//
// Response shape mirrors internal/agentruntime/runner/blueprint.go
// WorkspaceBlueprint so the runtime decodes it directly without any
// translation layer.
func GetWorkspaceBlueprint(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		signingKey := strings.TrimSpace(c.MCPSigningKey)
		if signingKey == "" {
			http.Error(w, "internal: signing key not configured", http.StatusServiceUnavailable)
			return
		}

		userID, workspaceID, ok := decodeBearerIdentity(r, signingKey)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Query-string workspace_id overrides for routing only; identity
		// from the token is authoritative. If the caller passes a
		// mismatching workspace_id we reject it to make drift loud
		// instead of silent.
		requestedWorkspace := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
		if requestedWorkspace != "" && requestedWorkspace != workspaceID {
			http.Error(w, "workspace_id in query does not match bearer token", http.StatusForbidden)
			return
		}
		_ = chi.URLParam // chi import kept for parity with other handlers; no URL params here

		sess := c.NewSession()
		agents, mErr := c.ChatService.ListAgents(r.Context(), &orchestratorservice.ListAgentsOpts{
			Sess:        sess,
			UserID:      userID,
			WorkspaceID: workspaceID,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		// Enrich each agent with its settings (model + allowed tools).
		// Fall through on per-agent settings errors: log once and return
		// the agent with default values so a partial settings outage
		// doesn't prevent the runtime from booting.
		blueprints := make([]internalAgentBlueprint, 0, len(agents))
		for _, agent := range agents {
			b := internalAgentBlueprint{
				Slug:         agent.Slug,
				Role:         agent.Role,
				SystemPrompt: agent.SystemPrompt,
				Description:  agent.Description,
			}
			settings, settingsErr := c.AgentService.GetAgentSettings(r.Context(), &orchestratorservice.GetAgentSettingsOpts{
				Sess:    c.NewSession(),
				UserID:  userID,
				AgentID: agent.ID,
			})
			if settingsErr != nil {
				c.Logger.Warn("blueprint: failed to load agent settings, using defaults",
					"workspace_id", workspaceID,
					"agent_id", agent.ID,
					"error", settingsErr,
				)
			} else if settings != nil {
				b.Model = settings.Model.ID
				// AllowedTools comes from agent_settings.allowed_tools
				// in Postgres. When the column is empty the runtime
				// falls back to each agent's hardcoded default toolset
				// (e.g. Wally's web_fetch + web_search_tool pair).
				// Forwarding the slice verbatim — the runtime decides
				// enforcement policy, not the orchestrator.
				if len(settings.AllowedTools) > 0 {
					b.AllowedTools = settings.AllowedTools
				}
			}
			blueprints = append(blueprints, b)
		}

		WriteJSON(w, http.StatusOK, internalWorkspaceBlueprint{
			WorkspaceID: workspaceID,
			Agents:      blueprints,
		})
	}
}

// internalWorkspaceBlueprint is the wire shape returned by
// GET /v1/internal/agents. Field names MUST match
// internal/agentruntime/runner/blueprint.go WorkspaceBlueprint so the
// runtime decodes the response directly. Keeping the type private to
// this file (no export) because the only valid consumer is the
// runtime's blueprint client.
type internalWorkspaceBlueprint struct {
	WorkspaceID string                   `json:"workspace_id"`
	Agents      []internalAgentBlueprint `json:"agents"`
}

type internalAgentBlueprint struct {
	Slug         string   `json:"slug"`
	Role         string   `json:"role"`
	SystemPrompt string   `json:"system_prompt"`
	Description  string   `json:"description"`
	AllowedTools []string `json:"allowed_tools"`
	Model        string   `json:"model"`
}

// decodeBearerIdentity extracts an HMAC bearer token from the request
// and returns the (userID, workspaceID) it encodes. Matches the scheme
// used by internal/orchestrator/mcp/server.go:66 so runtime pods can
// use the same token for both the MCP bridge and this endpoint.
func decodeBearerIdentity(r *http.Request, signingKey string) (userID, workspaceID string, ok bool) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return "", "", false
	}
	// Accept "Bearer <token>" (case-insensitive) or a bare token.
	if idx := strings.IndexByte(auth, ' '); idx >= 0 {
		scheme := strings.ToLower(strings.TrimSpace(auth[:idx]))
		if scheme != "bearer" {
			return "", "", false
		}
		auth = strings.TrimSpace(auth[idx+1:])
	}
	if auth == "" {
		return "", "", false
	}
	uid, wsid, err := crawblhmac.ValidateToken(signingKey, auth)
	if err != nil {
		return "", "", false
	}
	return uid, wsid, true
}
