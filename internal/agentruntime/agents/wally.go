package agents

import (
	"fmt"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	adktool "google.golang.org/adk/tool"
)

// WallyName is the canonical identifier for the research agent.
// Matches the orchestrator-side agent blueprint slug and the mobile
// app's agent id.
const WallyName = "wally"

// NewWally constructs the research agent from a blueprint entry. The
// instruction and description come from the orchestrator's
// agent_prompts / agents rows; Wally's Go source holds no copy. The
// toolbox currently wires web_fetch locally plus the shared MCP
// toolset for orchestrator-mediated tools.
func NewWally(model adkmodel.LLM, mcpToolset adktool.Toolset, instruction, description string) (adkagent.Agent, error) {
	if model == nil {
		return nil, fmt.Errorf("agents: Wally requires a non-nil model.LLM")
	}
	if instruction == "" {
		return nil, fmt.Errorf("agents: Wally requires a non-empty instruction from the blueprint")
	}

	webFetch, err := NewWebFetchTool()
	if err != nil {
		return nil, fmt.Errorf("agents: Wally tool setup: %w", err)
	}

	cfg := llmagent.Config{
		Name:        WallyName,
		Description: description,
		Instruction: instruction,
		Model:       model,
		Tools:       []adktool.Tool{webFetch},
	}
	if mcpToolset != nil {
		cfg.Toolsets = []adktool.Toolset{mcpToolset}
	}

	w, err := llmagent.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("agents: construct Wally llmagent: %w", err)
	}
	return w, nil
}
