// Package agents constructs the concrete ADK llmagents that make up
// a Crawbl user swarm. Phase 1 ships three agents — Manager (root,
// LLM-driven router via SubAgents delegation), Wally (research), and
// Eve (scheduling) — and they all share the same model adapter, MCP
// toolset, and local tool slice. The only per-agent differences are
// the slug, the instruction text pulled from the workspace blueprint,
// and whether the agent has SubAgents (only Manager does).
//
// All three constructors go through a single newLLMAgent helper so
// adding a new default agent is a ~5-line wrapper, not a copy-paste
// of the full llmagent.Config plumbing. The package still exposes
// typed NewManager / NewWally / NewEve functions because runner's
// BuildGraph pipeline references them by name — having a concrete
// call site for each agent is worth the few lines of wrapper code.
package agents

import (
	"fmt"
	"strings"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	adktool "google.golang.org/adk/tool"
)

// Canonical slugs used to identify each default agent across the
// orchestrator agents table, the mobile client's agent picker, the
// blueprint routing field, and the ADK SubAgents delegation flow.
// Every mention of these names in Go code MUST reference the constant.
const (
	ManagerName = "manager"
	WallyName   = "wally"
	EveName     = "eve"
)

// newLLMAgent is the shared builder every default constructor
// delegates to. It applies the invariants that hold for every
// crawbl-agent-runtime agent (non-nil model, non-empty instruction,
// optional MCP toolset, shared local tool slice) and returns the
// wrapped error on any failure.
//
// subAgents is optional — pass nil for sub-agents, or the list of
// transferable sub-agents for root/manager agents.
func newLLMAgent(
	name string,
	model adkmodel.LLM,
	mcpToolset adktool.Toolset,
	instruction, description string,
	localTools []adktool.Tool,
	subAgents []adkagent.Agent,
) (adkagent.Agent, error) {
	if model == nil {
		return nil, fmt.Errorf("agents: %s requires a non-nil model.LLM", name)
	}
	if strings.TrimSpace(instruction) == "" {
		return nil, fmt.Errorf("agents: %s requires a non-empty instruction from the blueprint", name)
	}

	cfg := llmagent.Config{
		Name:        name,
		Description: description,
		Instruction: instruction,
		Model:       model,
		Tools:       localTools,
		SubAgents:   subAgents,
	}
	if mcpToolset != nil {
		cfg.Toolsets = []adktool.Toolset{mcpToolset}
	}

	agent, err := llmagent.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("agents: construct %s llmagent: %w", name, err)
	}
	return agent, nil
}

// NewWally constructs the research sub-agent. It has no SubAgents of
// its own; it answers the user directly (via web_fetch /
// web_search_tool) and hands control back to the Manager when a
// message is outside its scope.
func NewWally(
	model adkmodel.LLM,
	mcpToolset adktool.Toolset,
	instruction, description string,
	localTools []adktool.Tool,
) (adkagent.Agent, error) {
	return newLLMAgent(WallyName, model, mcpToolset, instruction, description, localTools, nil)
}

// NewEve constructs the scheduling sub-agent. Same shape as Wally.
// Eve's calendar/cron tool integration is a Week 2 follow-up; the
// current constructor only wires the common local tool slice.
func NewEve(
	model adkmodel.LLM,
	mcpToolset adktool.Toolset,
	instruction, description string,
	localTools []adktool.Tool,
) (adkagent.Agent, error) {
	return newLLMAgent(EveName, model, mcpToolset, instruction, description, localTools, nil)
}

// NewManager constructs the root routing agent for a Crawbl user
// swarm. Manager owns the two sub-agents via SubAgents delegation,
// and can answer directly using the common local tool slice + MCP
// toolset when a user message does not fit either specialist.
//
// wally and eve must be non-nil — they are the sub-agents Manager
// transfers to. The caller is expected to construct them first via
// NewWally / NewEve (the graph construction order in
// runner.BuildGraph).
func NewManager(
	model adkmodel.LLM,
	wally, eve adkagent.Agent,
	mcpToolset adktool.Toolset,
	instruction, description string,
	localTools []adktool.Tool,
) (adkagent.Agent, error) {
	if wally == nil || eve == nil {
		return nil, fmt.Errorf("agents: %s requires non-nil Wally and Eve sub-agents", ManagerName)
	}
	return newLLMAgent(ManagerName, model, mcpToolset, instruction, description, localTools, []adkagent.Agent{wally, eve})
}
