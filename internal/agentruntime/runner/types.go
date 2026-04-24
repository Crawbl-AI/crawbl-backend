package runner

import (
	"log/slog"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	adkrunner "google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	adktool "google.golang.org/adk/tool"
)

// AppName is the ADK runner's AppName parameter. ADK uses it as a
// namespace key for session storage and telemetry. We pin "crawbl"
// here so every session row and event is tagged consistently across
// restarts and shared Redis keyspaces.
const AppName = "crawbl"

// Runner is the crawbl-agent-runtime's wrapper around an ADK runner.
// It owns the constructed agent graph, one ADK runner per agent
// (sharing the injected session.Service), and exposes a single
// RunTurn entry point that routes each turn to the agent named by the
// Converse request.
//
// Runner is safe to reuse across concurrent Converse streams:
// adkrunner.Runner itself is concurrency-safe; session state is keyed
// by (userID, sessionID), so two users streaming at the same time get
// independent rows even when the same per-agent runner serves both.
type Runner struct {
	logger     *slog.Logger
	graph      *Graph
	rootRunner *adkrunner.Runner
	byAgent    map[string]*adkrunner.Runner
	// sess is the durable session service (Redis-backed in production)
	// shared across every per-agent runner. Close() calls the service's
	// Close so main.go can tear it down cleanly on shutdown.
	sess adksession.Service
}

// BuildOptions carries the already-constructed dependencies that New
// needs. Passing them in explicitly instead of building them here
// keeps the runner package free of direct LLM SDK / MCP / storage
// imports — main.go wires everything once and hands it over.
type BuildOptions struct {
	// Model is the LLM adapter constructed by model.NewFromConfig.
	Model adkmodel.LLM
	// MCPToolset is the orchestrator-mediated tool bridge. May be nil
	// for integration environments that do not exercise orchestrator
	// tools.
	MCPToolset adktool.Toolset
	// SessionService is the durable session service (Redis-backed in
	// production). Required — every per-agent runner shares this
	// instance so session history is a single conversation regardless
	// of which agent handles each turn.
	SessionService adksession.Service
	// Blueprint carries per-agent instruction and description text
	// sourced from the orchestrator's agents tables.
	Blueprint *WorkspaceBlueprint
	// LocalTools is the shared local tool slice (web_fetch,
	// web_search_tool) built once per pod by tools.BuildCommonTools
	// and handed to every agent constructor.
	LocalTools []adktool.Tool
	// Logger for the runner. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// Graph holds the constructed root agent plus references to the
// sub-agents in case the caller needs to log individual agent
// telemetry. The root is what the gRPC Converse handler runs on
// every user turn.
type Graph struct {
	// Root is the entry point for every user turn. Always the Manager
	// agent; the Manager routes to Wally/Eve via ADK's transfer_to_agent
	// flow based on the user's intent.
	Root adkagent.Agent

	// Manager, Wally, Eve are the individual agents in the graph.
	// Exposed so telemetry and blueprint refresh code can target them
	// by name. Callers should not invoke them directly — always go
	// through Root.
	Manager adkagent.Agent
	Wally   adkagent.Agent
	Eve     adkagent.Agent
}

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
