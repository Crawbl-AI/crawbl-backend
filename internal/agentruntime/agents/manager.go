package agents

import (
	"fmt"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	adktool "google.golang.org/adk/tool"
)

// ManagerName is the canonical slug Crawbl's orchestrator and mobile
// client use to target the root routing agent. The matching row in
// the orchestrator's agents table carries the same slug, along with
// the system prompt and description this constructor consumes from
// the WorkspaceBlueprint.
const ManagerName = "manager"

// NewManager constructs the root agent for a Crawbl user swarm from
// a blueprint entry. instruction and description come from the
// orchestrator's agent_prompts / agents rows (seeded via
// migrations/orchestrator/seed/agents.json); the constructor does not
// hold any hardcoded copy.
//
// Caller is responsible for passing a valid model.LLM, the two
// sub-agents (Wally + Eve), the shared MCP toolset, and the blueprint
// fields. All four must be non-nil for the LLM agent to build
// correctly.
func NewManager(model adkmodel.LLM, wally, eve adkagent.Agent, mcpToolset adktool.Toolset, instruction, description string) (adkagent.Agent, error) {
	if model == nil {
		return nil, fmt.Errorf("agents: Manager requires a non-nil model.LLM")
	}
	if wally == nil || eve == nil {
		return nil, fmt.Errorf("agents: Manager requires non-nil Wally and Eve sub-agents")
	}
	if instruction == "" {
		return nil, fmt.Errorf("agents: Manager requires a non-empty instruction from the blueprint")
	}

	cfg := llmagent.Config{
		Name:        ManagerName,
		Description: description,
		Instruction: instruction,
		Model:       model,
		SubAgents:   []adkagent.Agent{wally, eve},
	}
	if mcpToolset != nil {
		cfg.Toolsets = []adktool.Toolset{mcpToolset}
	}

	m, err := llmagent.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("agents: construct Manager llmagent: %w", err)
	}
	return m, nil
}
