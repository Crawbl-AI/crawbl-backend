package server

import orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"

// agentResponse represents an AI agent in API responses.
// Agents are individual swarm members that users can interact with through conversations.
type agentResponse struct {
	// ID is the unique identifier for the agent.
	ID string `json:"id"`

	// Name is the display name of the agent.
	Name string `json:"name"`

	// Role is the functional role or specialty of the agent (e.g., "assistant", "analyst").
	Role string `json:"role"`

	// Slug is the unique routing identifier for the agent.
	Slug string `json:"slug"`

	// Avatar is the URL to the agent's avatar image.
	Avatar string `json:"avatar"`

	// Status is the current availability status (e.g., "online", "offline", "busy").
	Status string `json:"status"`
}

// toAgentResponse converts a domain Agent to the API response format.
// Returns an empty response if the agent pointer is nil.
func toAgentResponse(agent *orchestrator.Agent) agentResponse {
	if agent == nil {
		return agentResponse{}
	}

	return agentResponse{
		ID:        agent.ID,
		Name:      agent.Name,
		Role:      agent.Role,
		Slug:      agent.Slug,
		Avatar:    agent.AvatarURL,
		Status:    string(agent.Status),
	}
}
