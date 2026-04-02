package chatservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// routingDecision is the JSON shape Manager returns when acting as a router.
// Agents is the ordered list of agent slugs that should respond to this message.
// Response is an optional inline reply: Manager uses it when routing to itself
// for a short/obvious answer, avoiding a second round-trip to the swarm.
type routingDecision struct {
	Agents   []string `json:"agents"`
	Response *string  `json:"response,omitempty"`
}

// buildRoutingPrompt constructs the system prompt injected into Manager's routing
// turn. The prompt is intentionally narrow: Manager must return ONLY a JSON object
// and nothing else, so the orchestrator can reliably parse its output without
// stripping prose or markdown fences.
//
// Manager itself is excluded from the "available sub-agents" list because it is
// the fallback — if no sub-agent fits, Manager answers alone (optionally inline).
func buildRoutingPrompt(agents []*orchestrator.Agent) string {
	var sb strings.Builder

	sb.WriteString("You are the swarm router. Your only job is to decide which agents should respond to the user's message.\n\n")
	sb.WriteString("Available sub-agents:\n")

	for _, a := range agents {
		// Skip Manager — it is listed separately as the solo fallback below.
		if a.Role == orchestrator.AgentRoleManager {
			continue
		}
		fmt.Fprintf(&sb, "- %s: %s\n", a.Slug, a.Description)
	}

	sb.WriteString("\nRules:\n")
	sb.WriteString("1. Return ONLY valid JSON. No explanation, no markdown, no prose.\n")
	sb.WriteString("2. If one or more sub-agents are the right fit, return: {\"agents\": [\"<slug>\", ...]}\n")
	sb.WriteString("3. If only Manager should respond, return: {\"agents\": [\"manager\"]}\n")
	sb.WriteString("4. If routing to manager solo AND the answer is short/obvious, include it inline:\n")
	sb.WriteString("   {\"agents\": [\"manager\"], \"response\": \"<short answer here>\"}\n")
	sb.WriteString("5. Do NOT include manager in the agents list together with sub-agents.\n")
	sb.WriteString("6. Use only the slugs listed above or \"manager\".\n")

	return sb.String()
}

// routeMessage asks Manager to decide which agents should handle the message.
// It injects a routing-only system prompt so Manager acts purely as a dispatcher
// rather than generating a full answer in this turn.
//
// The conversation's SessionID is reused so ZeroClaw can maintain context across
// the routing turn and the subsequent reply turns.
//
// Falls back to ["manager"] on any parse failure or if all returned slugs are
// unknown — guarantees the caller always gets at least one valid agent to invoke.
func (s *service) routeMessage(
	ctx context.Context,
	runtimeState *orchestrator.RuntimeStatus,
	conversationID string,
	message string,
	agents []*orchestrator.Agent,
) (*routingDecision, *merrors.Error) {
	routingPrompt := buildRoutingPrompt(agents)

	turns, mErr := s.runtimeClient.SendText(ctx, &userswarmclient.SendTextOpts{
		Runtime:      runtimeState,
		Message:      message,
		SessionID:    conversationID + ":routing",
		SystemPrompt: routingPrompt,
	})
	if mErr != nil {
		return nil, mErr
	}

	// Use the first turn's text as the routing response.
	// Manager is the entry-point agent; its turn comes first.
	var raw string
	if len(turns) > 0 {
		raw = strings.TrimSpace(turns[0].Text)
	}

	fallback := &routingDecision{Agents: []string{"manager"}}

	if raw == "" {
		return fallback, nil
	}

	var decision routingDecision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		// Manager returned prose or malformed JSON — route to manager so the
		// user still gets a response rather than a hard error.
		return fallback, nil
	}

	if len(decision.Agents) == 0 {
		return fallback, nil
	}

	// Build a slug set from the known agents so we can reject phantom slugs.
	// "manager" is always valid even if not present in the agents slice.
	knownSlugs := make(map[string]struct{}, len(agents)+1)
	knownSlugs["manager"] = struct{}{}
	for _, a := range agents {
		knownSlugs[a.Slug] = struct{}{}
	}

	validated := decision.Agents[:0]
	for _, slug := range decision.Agents {
		if _, ok := knownSlugs[slug]; ok {
			validated = append(validated, slug)
		}
	}

	if len(validated) == 0 {
		return fallback, nil
	}

	decision.Agents = validated
	return &decision, nil
}
