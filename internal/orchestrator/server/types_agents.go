package server

import (
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

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
		ID:     agent.ID,
		Name:   agent.Name,
		Role:   agent.Role,
		Slug:   agent.Slug,
		Avatar: agent.AvatarURL,
		Status: string(agent.Status),
	}
}

// agentDetailResponse is the full agent profile returned by GET /v1/agents/{id}/details.
type agentDetailResponse struct {
	ID          string             `json:"id"`
	WorkspaceID string             `json:"workspace_id"`
	Name        string             `json:"name"`
	Role        string             `json:"role"`
	Slug        string             `json:"slug"`
	CreatedAt   string             `json:"created_at"`
	UpdatedAt   *string            `json:"updated_at"`
	Description string             `json:"description"`
	AvatarURL   string             `json:"avatar_url"`
	Status      string             `json:"status"`
	SortOrder   int                `json:"sort_order"`
	Stats       agentStatsResponse `json:"stats"`
}

type agentStatsResponse struct {
	TotalMessages int `json:"total_messages"`
}

type agentToolResponse struct {
	Name        string                    `json:"name"`
	DisplayName string                    `json:"display_name"`
	Description string                    `json:"description"`
	Category    agentToolCategoryResponse `json:"category"`
	IconURL     string                    `json:"icon_url"`
}

type agentToolCategoryResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
}

type agentModelResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type agentSettingsResponse struct {
	Model          agentModelResponse    `json:"model"`
	ResponseLength string                `json:"response_length"`
	Prompts        []agentPromptResponse `json:"prompts"`
}

type agentPromptResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

type agentHistoryItemResponse struct {
	ConversationID string  `json:"conversation_id"`
	Title          string  `json:"title"`
	Subtitle       string  `json:"subtitle"`
	CreatedAt      *string `json:"created_at"`
}

type offsetPaginationResponse struct {
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasNext bool `json:"has_next"`
}

// toAgentDetailResponse converts a domain AgentDetails to the API response format.
func toAgentDetailResponse(d *orchestrator.AgentDetails) agentDetailResponse {
	resp := agentDetailResponse{
		ID:          d.ID,
		WorkspaceID: d.WorkspaceID,
		Name:        d.Name,
		Role:        d.Role,
		Slug:        d.Slug,
		CreatedAt:   d.CreatedAt.Format(time.RFC3339),
		Description: d.Description,
		AvatarURL:   d.AvatarURL,
		Status:      string(d.Status),
		SortOrder:   d.SortOrder,
		Stats: agentStatsResponse{
			TotalMessages: d.Stats.TotalMessages,
		},
	}
	if !d.UpdatedAt.IsZero() {
		t := d.UpdatedAt.Format(time.RFC3339)
		resp.UpdatedAt = &t
	}
	return resp
}

// toAgentToolResponse converts a domain AgentTool to the API response format.
func toAgentToolResponse(t orchestrator.AgentTool) agentToolResponse {
	return agentToolResponse{
		Name:        t.Name,
		DisplayName: t.DisplayName,
		Description: t.Description,
		Category: agentToolCategoryResponse{
			ID:       t.Category.ID,
			Name:     t.Category.Name,
			ImageURL: t.Category.ImageURL,
		},
		IconURL: t.IconURL,
	}
}
