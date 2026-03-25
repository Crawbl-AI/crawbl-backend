package server

// agentResponse represents an AI agent in API responses.
// Agents are individual swarm members that users can interact with through conversations.
type agentResponse struct {
	// ID is the unique identifier for the agent.
	ID string `json:"id"`

	// Name is the display name of the agent.
	Name string `json:"name"`

	// Role is the functional role or specialty of the agent (e.g., "assistant", "analyst").
	Role string `json:"role"`

	// Avatar is the URL to the agent's avatar image.
	Avatar string `json:"avatar"`

	// Status is the current availability status (e.g., "online", "offline", "busy").
	Status string `json:"status"`

	// HasUpdate indicates whether the agent has new content or status changes
	// that the user hasn't seen yet.
	HasUpdate bool `json:"hasUpdate"`
}
