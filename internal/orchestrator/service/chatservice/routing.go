package chatservice

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

// routingDecision is the JSON shape returned by the Routing LLM.
// Type is "simple" (Manager answers) or "group" (sub-agents work in parallel).
type routingDecision struct {
	Type  string      `json:"type"`            // "simple" | "group"
	Tasks []agentTask `json:"tasks,omitempty"` // only when type="group"
}

// agentTask assigns a specific task to a sub-agent for group requests.
type agentTask struct {
	Slug string `json:"slug"` // agent slug (e.g. "wally", "eve")
	Task string `json:"task"` // specific task description for the agent
}

const (
	routingTypeSimple = "simple"
	routingTypeGroup  = "group"
)

// buildRoutingPrompt constructs the system prompt for the Routing LLM.
// The prompt is intentionally minimal (~50 tokens output) — the Routing LLM
// is an infrastructure switch, not an agent. It has no context, no memory,
// no tools. It only sees the message text and the agent list.
func buildRoutingPrompt(agents []*orchestrator.Agent) string {
	var sb strings.Builder

	sb.WriteString("You are a router. Decide: is this a simple request (one agent answers) or group (multiple agents needed)?\n\n")
	sb.WriteString("Available sub-agents:\n")

	for _, a := range agents {
		if a.Role == orchestrator.AgentRoleManager {
			continue
		}
		fmt.Fprintf(&sb, "- %s: %s\n", a.Slug, a.Description)
	}

	sb.WriteString("\nRules:\n")
	sb.WriteString("1. Return ONLY valid JSON. No explanation, no markdown, no prose.\n")
	sb.WriteString("2. Simple: {\"type\":\"simple\"}\n")
	sb.WriteString("3. Group: {\"type\":\"group\",\"tasks\":[{\"slug\":\"<agent>\",\"task\":\"<specific task>\"}]}\n")
	sb.WriteString("4. ALWAYS use GROUP if the message contains ANY of these signals:\n")
	sb.WriteString("   - Plural words: \"guys\", \"team\", \"both\", \"all\", \"them\", \"everyone\", \"y'all\", \"ребят\", \"команда\", \"вы все\"\n")
	sb.WriteString("   - Requests for opinions/perspectives from multiple people\n")
	sb.WriteString("   - Tasks that combine research + creative work\n")
	sb.WriteString("   - \"help me\" or \"prepare\" type requests (multiple agents can contribute)\n")
	sb.WriteString("5. Use SIMPLE ONLY for direct questions with one clear answer (\"what time is it\", \"who are you\", \"translate this\").\n")
	sb.WriteString("6. When in doubt, choose GROUP — it's better to involve agents than to miss a collaboration opportunity.\n")
	sb.WriteString("7. For group: assign a SPECIFIC task to each agent based on their description.\n")
	sb.WriteString("8. Use only the slugs listed above.\n")

	return sb.String()
}

// parseRoutingResponse parses the Routing LLM's JSON response into a routingDecision.
// Returns the default (simple) decision on any parse failure — guarantees the caller
// always gets a valid decision.
func parseRoutingResponse(raw string, agents []*orchestrator.Agent, logger *slog.Logger) *routingDecision {
	fallback := &routingDecision{Type: routingTypeSimple}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}

	var decision routingDecision
	if err := json.Unmarshal([]byte(trimmed), &decision); err != nil {
		logger.Warn("routing: failed to parse LLM response, falling back to simple",
			"raw", trimmed,
			"error", err.Error(),
		)
		return fallback
	}

	if decision.Type != routingTypeSimple && decision.Type != routingTypeGroup {
		return fallback
	}

	if decision.Type == routingTypeSimple {
		return &decision
	}

	// Validate group tasks — filter to known agent slugs.
	knownSlugs := make(map[string]struct{}, len(agents))
	for _, a := range agents {
		if a.Role != orchestrator.AgentRoleManager {
			knownSlugs[a.Slug] = struct{}{}
		}
	}

	validated := decision.Tasks[:0]
	for _, t := range decision.Tasks {
		if _, ok := knownSlugs[t.Slug]; ok {
			validated = append(validated, t)
		}
	}

	if len(validated) == 0 {
		return fallback
	}

	decision.Tasks = validated
	return &decision
}
