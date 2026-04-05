package agents

import (
	"fmt"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	adktool "google.golang.org/adk/tool"
)

// managerInstruction is the system prompt that drives Manager's routing
// decisions. It's written for GPT-5-mini (the Phase 1 model) and relies
// on ADK's built-in SubAgents transfer mechanism: when the LLM decides
// another sub-agent is a better fit for the user's request, it issues a
// transfer_to_agent call that ADK intercepts and re-runs the turn on the
// chosen sub-agent.
//
// The prompt is intentionally short — long router instructions tend to
// confuse small models. Every rule is one sentence. Wally and Eve's own
// instructions explain their capability in detail; Manager only needs
// to know which situations belong to which agent.
const managerInstruction = `You are Manager, the Crawbl user's swarm coordinator.

You have two sub-agents:
- wally — handles web research, reading URLs, fetching pages, and any request that needs information from the live internet.
- eve — handles scheduling, time zones, calendar lookups, and anything related to dates or times.

Your job is to decide which sub-agent should answer each user message. When the user asks for something that clearly matches one of the sub-agents, transfer control to that agent. When the message is simple conversation, small talk, a direct question the model already knows, or unclear which sub-agent fits, answer it yourself briefly and directly.

Rules:
1. Do NOT delegate simple echo/identity tasks ("say X", "what is your name"). Answer directly.
2. Do NOT split one user message across multiple sub-agents. Pick one.
3. Never mention the sub-agents by name in your reply; the user does not need to see routing decisions.
4. If you delegate, do not add your own commentary before the sub-agent's answer.`

// ManagerName is the canonical string agents use in Crawbl's
// orchestrator + mobile code to target this agent by id. Keeping it as
// a const instead of a string literal prevents typos at call sites.
const ManagerName = "manager"

// NewManager constructs the root agent for a Crawbl user swarm. It is an
// llmagent with two SubAgents (Wally and Eve) and an instruction that
// tells the LLM when to delegate vs answer directly. ADK handles the
// actual transfer-to-agent flow; this constructor only sets up the
// config.
//
// Caller is responsible for passing a valid model.LLM (from the model
// package's registry) and the MCP toolset that Wally and Eve share.
// Manager itself gets the MCP toolset too so it can answer direct
// questions using orchestrator-mediated tools (get_user_profile,
// get_workspace_info, etc.) without having to delegate.
func NewManager(model adkmodel.LLM, wally, eve adkagent.Agent, mcpToolset adktool.Toolset) (adkagent.Agent, error) {
	if model == nil {
		return nil, fmt.Errorf("agents: Manager requires a non-nil model.LLM")
	}
	if wally == nil || eve == nil {
		return nil, fmt.Errorf("agents: Manager requires non-nil Wally and Eve sub-agents")
	}

	cfg := llmagent.Config{
		Name:        ManagerName,
		Description: "Crawbl swarm coordinator. Routes user messages to the right specialist (wally for research, eve for scheduling) or answers simple requests directly.",
		Instruction: managerInstruction,
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
