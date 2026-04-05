// Package runner owns the construction of the multi-agent graph that
// each crawbl-agent-runtime pod serves. It wires the model adapter,
// the MCP toolset, and the sub-agents defined in the WorkspaceBlueprint
// into a single root Agent that the gRPC Converse handler invokes on
// every user turn.
//
// Multi-agent composition uses ADK's SubAgents-based LLM-driven
// delegation, not sequentialagent. sequentialagent runs every
// sub-agent in order regardless of content; we want the LLM to pick
// exactly one sub-agent per turn based on the user's intent.
// llmagent.Config.SubAgents is that primitive — the root agent's LLM
// emits a transfer_to_agent tool call and ADK re-runs the turn on the
// chosen sub-agent.
//
// Composition shape:
//
//	                Manager (llmagent, root)
//	               /                         \
//	            Wally (research)           Eve (scheduling)
//	        Tools: web_fetch              Tools: (none)
//	        Toolsets: mcp                 Toolsets: mcp
//
// Every agent shares the same model.LLM and the same MCP toolset so
// Manager can answer directly using orchestrator tools without always
// delegating. The instruction and description strings for each agent
// come from the orchestrator's agents + agent_prompts rows (seeded via
// migrations/orchestrator/seed/agents.json) and arrive here through
// the WorkspaceBlueprint. No agent text lives in Go source.
package runner

import (
	"fmt"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	adktool "google.golang.org/adk/tool"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/agents"
)

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

// BuildGraph constructs the three-agent graph from a model.LLM, an
// optional MCP toolset, and a WorkspaceBlueprint. The blueprint
// supplies per-agent instruction + description text, sourced from the
// orchestrator's agents / agent_prompts tables.
//
// The construction order matters: Wally and Eve must exist before
// Manager because Manager's Config.SubAgents takes them by reference.
// If any sub-agent fails to construct, BuildGraph returns the
// underlying error and the caller aborts startup.
func BuildGraph(model adkmodel.LLM, mcpToolset adktool.Toolset, bp *WorkspaceBlueprint) (*Graph, error) {
	if model == nil {
		return nil, fmt.Errorf("runner: BuildGraph requires a non-nil model.LLM")
	}
	if bp == nil {
		return nil, fmt.Errorf("runner: BuildGraph requires a non-nil WorkspaceBlueprint")
	}

	managerBP, managerOK := bp.agentBySlug(agents.ManagerName)
	wallyBP, wallyOK := bp.agentBySlug(agents.WallyName)
	eveBP, eveOK := bp.agentBySlug(agents.EveName)
	if !managerOK || !wallyOK || !eveOK {
		return nil, fmt.Errorf("runner: blueprint missing required agents (manager=%v wally=%v eve=%v)", managerOK, wallyOK, eveOK)
	}

	wally, err := agents.NewWally(model, mcpToolset, wallyBP.SystemPrompt, wallyBP.Description)
	if err != nil {
		return nil, fmt.Errorf("runner: build Wally: %w", err)
	}

	eve, err := agents.NewEve(model, mcpToolset, eveBP.SystemPrompt, eveBP.Description)
	if err != nil {
		return nil, fmt.Errorf("runner: build Eve: %w", err)
	}

	manager, err := agents.NewManager(model, wally, eve, mcpToolset, managerBP.SystemPrompt, managerBP.Description)
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
