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
// agent_prompts / agents rows; Wally's Go source holds no copy.
// localTools is the shared local tool slice (web_fetch,
// web_search_tool, memory_*) built by agents.BuildCommonTools.
func NewWally(model adkmodel.LLM, mcpToolset adktool.Toolset, instruction, description string, localTools []adktool.Tool) (adkagent.Agent, error) {
	if model == nil {
		return nil, fmt.Errorf("agents: Wally requires a non-nil model.LLM")
	}
	if instruction == "" {
		return nil, fmt.Errorf("agents: Wally requires a non-empty instruction from the blueprint")
	}

	cfg := llmagent.Config{
		Name:        WallyName,
		Description: description,
		Instruction: instruction,
		Model:       model,
		Tools:       localTools,
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
