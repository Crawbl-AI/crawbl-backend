package agents

import (
	"fmt"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	adktool "google.golang.org/adk/tool"
)

// WallyName is the canonical identifier for the research agent. Matches
// the orchestrator-side agent blueprint slug and the mobile app's agent
// id.
const WallyName = "wally"

// wallyInstruction is Wally's system prompt. It explains the agent's
// role and how to use the web_fetch tool.
const wallyInstruction = `You are Wally, Crawbl's research specialist.

You help the user find information from the live internet. When the user asks about a specific URL, a fact, a news item, a product, or anything that might need current web content, call the web_fetch tool to read the page, then summarize or extract exactly what the user asked for.

Rules:
1. Prefer citing the URL you fetched in your response so the user can verify.
2. Do not fabricate URLs or contents. If the fetch fails, say so and suggest an alternative.
3. Keep responses focused — summarize the page, don't paste large HTML dumps.
4. If the user asks for the page title, return just the title (and the URL).
5. If the user's message is not a research task, defer to the Manager by saying you cannot help with that specific request.`

// NewWally constructs the research agent. Wally's toolbox includes
// web_fetch (the only locally-implemented tool in Phase 1) plus the
// shared MCP toolset for orchestrator-mediated tools. Wally can
// transfer back to Manager if the user's request doesn't fit (via
// ADK's built-in transfer_to_parent flow).
func NewWally(model adkmodel.LLM, mcpToolset adktool.Toolset) (adkagent.Agent, error) {
	if model == nil {
		return nil, fmt.Errorf("agents: Wally requires a non-nil model.LLM")
	}

	webFetch, err := NewWebFetchTool()
	if err != nil {
		return nil, fmt.Errorf("agents: Wally tool setup: %w", err)
	}

	cfg := llmagent.Config{
		Name:        WallyName,
		Description: "Research specialist. Fetches web pages and extracts information for the user.",
		Instruction: wallyInstruction,
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
