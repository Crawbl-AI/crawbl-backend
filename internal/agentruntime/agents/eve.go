package agents

import (
	"fmt"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	adktool "google.golang.org/adk/tool"
)

// EveName is the canonical identifier for the scheduling agent.
// Matches the orchestrator-side agent blueprint slug and the mobile
// app's agent id.
const EveName = "eve"

// NewEve constructs the scheduling agent from a blueprint entry. The
// instruction and description come from the orchestrator's
// agent_prompts / agents rows; Eve's Go source holds no copy. Eve
// shares the common local tool slice (for memory_* and web_search)
// plus the MCP toolset so it can use orchestrator tools (for example
// to look up the user's workspace time zone via get_user_profile).
func NewEve(model adkmodel.LLM, mcpToolset adktool.Toolset, instruction, description string, localTools []adktool.Tool) (adkagent.Agent, error) {
	if model == nil {
		return nil, fmt.Errorf("agents: Eve requires a non-nil model.LLM")
	}
	if instruction == "" {
		return nil, fmt.Errorf("agents: Eve requires a non-empty instruction from the blueprint")
	}

	cfg := llmagent.Config{
		Name:        EveName,
		Description: description,
		Instruction: instruction,
		Model:       model,
		Tools:       localTools,
	}
	if mcpToolset != nil {
		cfg.Toolsets = []adktool.Toolset{mcpToolset}
	}

	e, err := llmagent.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("agents: construct Eve llmagent: %w", err)
	}
	return e, nil
}
