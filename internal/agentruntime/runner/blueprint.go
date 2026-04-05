package runner

import (
	"context"
	"log/slog"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/agents"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
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

// FetchBlueprint returns the WorkspaceBlueprint the runtime should use
// to build its agent graph. In Phase 1 this returns a hardcoded default
// matching the Manager+Wally+Eve graph constructed by BuildGraph; Phase
// 2 replaces the body with an HMAC-authed HTTP GET against the
// orchestrator's forthcoming GET /v1/internal/agents endpoint.
//
// The ctx is already ready for an HTTP call (Phase 2) — today it's
// unused but accepting it now keeps the signature stable across the
// swap so callers don't need to change.
//
// Returns a non-nil blueprint on success; on transport/decode failure
// in Phase 2 it will return the zero value plus an error, and the
// caller is expected to fall back to DefaultBlueprint.
func FetchBlueprint(ctx context.Context, cfg config.Config, logger *slog.Logger) (*WorkspaceBlueprint, error) {
	_ = ctx
	if logger == nil {
		logger = slog.Default()
	}
	// Phase 1 stub: no orchestrator call, log + return default.
	//
	// This is deliberately load-bearing: US-AR-010 lands the contract
	// (types + call site + fallback) without touching the orchestrator.
	// Phase 2's atomic swap adds the HTTP handler in the orchestrator
	// and flips the body here to a real call with one commit. Until
	// then the log line surfaces a clear breadcrumb for operators who
	// expect dynamic blueprint loading.
	logger.Info(
		"runner: fetching workspace blueprint (Phase 1 hardcoded default — real HTTP fetch lands in Phase 2)",
		"workspace_id", cfg.WorkspaceID,
		"endpoint", "GET "+cfg.OrchestratorGRPCEndpoint+"/v1/internal/agents?workspace_id="+cfg.WorkspaceID+" (NOT YET IMPLEMENTED)",
	)
	return DefaultBlueprint(cfg.WorkspaceID), nil
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
