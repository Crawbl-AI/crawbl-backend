package agents

import (
	"fmt"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	adktool "google.golang.org/adk/tool"
)

// EveName is the canonical identifier for the scheduling agent. Matches
// the orchestrator-side agent blueprint slug and the mobile app's agent
// id.
const EveName = "eve"

// eveInstruction is Eve's system prompt. Phase 1 doesn't ship any
// calendar tools yet (they land in Phase 3 via the cron_* catalog
// entries and the orchestrator's Gmail/Calendar OAuth adapters), so
// Eve answers from the model's own world knowledge for now.
const eveInstruction = `You are Eve, Crawbl's scheduling and time specialist.

You help the user with anything involving dates, times, time zones, scheduling conventions, and calendar concepts. Phase 1 of Crawbl does not yet give you live calendar access — lean on your general knowledge of time zones and date math to answer the user directly.

Rules:
1. When asked for the current time in a city, compute it from the user's implied now and the city's standard time zone offset. State the time zone abbreviation alongside the number.
2. When asked to plan or suggest scheduling, respond with clear structured output (a short list).
3. If the user asks for a live calendar lookup or to create an event, explain that calendar integration is coming soon and offer to help with the planning part instead.
4. If the user's message is not about time or scheduling, defer to the Manager by saying you cannot help with that specific request.`

// NewEve constructs the scheduling agent. Eve shares the MCP toolset
// with the other agents so it can use orchestrator tools (e.g. get the
// user's workspace time zone via get_user_profile), but has no Phase 1
// local tools — calendar integration lands in Phase 3.
func NewEve(model adkmodel.LLM, mcpToolset adktool.Toolset) (adkagent.Agent, error) {
	if model == nil {
		return nil, fmt.Errorf("agents: Eve requires a non-nil model.LLM")
	}

	cfg := llmagent.Config{
		Name:        EveName,
		Description: "Scheduling and time specialist. Handles time zones, date math, and scheduling conventions.",
		Instruction: eveInstruction,
		Model:       model,
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
