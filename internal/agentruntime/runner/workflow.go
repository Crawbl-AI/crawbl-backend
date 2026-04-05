// Package runner owns the construction of the multi-agent graph that
// each crawbl-agent-runtime pod serves. It wires the model adapter,
// the MCP toolset, and the three agents (Manager + Wally + Eve) into
// a single root Agent that the gRPC Converse handler (US-AR-009)
// invokes on every user turn.
//
// Plan §5 decision: multi-agent composition uses ADK's SubAgents-based
// LLM-driven delegation, NOT sequentialagent. sequentialagent runs every
// sub-agent in order regardless of content; we want the LLM to pick
// exactly one sub-agent per turn based on the user's intent. ADK's
// llmagent.Config.SubAgents is that primitive — the root agent's LLM
// emits a transfer_to_agent tool call and ADK re-runs the turn on the
// chosen sub-agent.
//
// Composition shape (Phase 1):
//
//	                Manager (llmagent, root)
//	               /                         \
//	            Wally (research)           Eve (scheduling)
//	        Tools: web_fetch              Tools: (none)
//	        Toolsets: mcp                 Toolsets: mcp
//
// Every agent shares the same model.LLM and the same MCP toolset so
// Manager can answer directly using orchestrator tools without always
// delegating. US-AR-010's workspace blueprint bootstrap will later
// swap the hardcoded Manager/Wally/Eve here for Postgres-driven
// agent definitions, but Phase 1 hardcodes them.
package runner

import (
	"fmt"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	adktool "google.golang.org/adk/tool"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/agents"
)

// Graph holds the constructed root agent plus optional references to
// the sub-agents in case the caller needs to log individual agent
// telemetry. The root is all US-AR-009 needs to run Converse turns.
type Graph struct {
	// Root is the entry point for every user turn. In Phase 1 this is
	// always the Manager agent, which routes to Wally/Eve as needed.
	Root adkagent.Agent

	// Manager, Wally, Eve are the individual agents in the graph.
	// Exposed so telemetry and blueprint refresh code can target them
	// by name. Callers should not invoke them directly — always go
	// through Root.
	Manager adkagent.Agent
	Wally   adkagent.Agent
	Eve     adkagent.Agent
}

// BuildGraph constructs the Phase 1 three-agent graph from a model.LLM
// and an optional MCP toolset. The toolset may be nil in tests that
// don't need orchestrator-mediated tools; production always passes a
// non-nil value from tools/mcp.Toolset.
//
// The construction order matters: Wally and Eve must exist before
// Manager because Manager's Config.SubAgents takes them by reference.
// If either sub-agent fails to construct, BuildGraph returns the
// underlying error and the caller aborts startup.
func BuildGraph(model adkmodel.LLM, mcpToolset adktool.Toolset) (*Graph, error) {
	if model == nil {
		return nil, fmt.Errorf("runner: BuildGraph requires a non-nil model.LLM")
	}

	wally, err := agents.NewWally(model, mcpToolset)
	if err != nil {
		return nil, fmt.Errorf("runner: build Wally: %w", err)
	}

	eve, err := agents.NewEve(model, mcpToolset)
	if err != nil {
		return nil, fmt.Errorf("runner: build Eve: %w", err)
	}

	manager, err := agents.NewManager(model, wally, eve, mcpToolset)
	if err != nil {
		return nil, fmt.Errorf("runner: build Manager: %w", err)
	}

	return &Graph{
		Root:    manager,
		Manager: manager,
		Wally:   wally,
		Eve:     eve,
	}, nil
}
