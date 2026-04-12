package dto

import (
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

// AgentResponse represents an AI agent in API responses.
// Agents are individual swarm members that users can interact with through conversations.
type AgentResponse struct {
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

// ToAgentResponse converts a domain Agent to the API response format.
// Returns an empty response if the agent pointer is nil.
func ToAgentResponse(agent *orchestrator.Agent) AgentResponse {
	if agent == nil {
		return AgentResponse{}
	}

	return AgentResponse{
		ID:     agent.ID,
		Name:   agent.Name,
		Role:   agent.Role,
		Slug:   agent.Slug,
		Avatar: agent.AvatarURL,
		Status: string(agent.Status),
	}
}

// AgentDetailResponse is the full agent profile returned by GET /v1/agents/{id}/details.
type AgentDetailResponse struct {
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
	Skills      []string           `json:"skills"`
	Stats       AgentStatsResponse `json:"stats"`
}

// AgentStatsResponse contains aggregate statistics for an agent.
type AgentStatsResponse struct {
	TotalMessages         int   `json:"total_messages"`
	TotalTokensUsed       int64 `json:"total_tokens_used"`
	TotalPromptTokens     int64 `json:"total_prompt_tokens"`
	TotalCompletionTokens int64 `json:"total_completion_tokens"`
	TotalRequests         int   `json:"total_requests"`
}

// AgentToolResponse represents a tool available to an agent.
type AgentToolResponse struct {
	Name        string                    `json:"name"`
	DisplayName string                    `json:"display_name"`
	Description string                    `json:"description"`
	Category    AgentToolCategoryResponse `json:"category"`
	IconURL     string                    `json:"icon_url"`
}

// AgentToolCategoryResponse represents the category of an agent tool.
type AgentToolCategoryResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
}

// AgentModelResponse represents an LLM model available to an agent.
type AgentModelResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AgentSettingsResponse represents the settings for an agent.
type AgentSettingsResponse struct {
	Model          string                `json:"model"`
	ResponseLength string                `json:"response_length"`
	Prompts        []AgentPromptResponse `json:"prompts"`
}

// AgentPromptResponse represents a prompt configured for an agent.
type AgentPromptResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

// AgentHistoryItemResponse represents a single item in an agent's conversation history.
type AgentHistoryItemResponse struct {
	ConversationID string  `json:"conversation_id"`
	Title          string  `json:"title"`
	Subtitle       string  `json:"subtitle"`
	CreatedAt      *string `json:"created_at"`
}

// AgentHistoryResponse is the response envelope for GET /v1/agents/{id}/history.
type AgentHistoryResponse struct {
	Items      []AgentHistoryItemResponse `json:"items"`
	Pagination OffsetPaginationResponse   `json:"pagination"`
}

// AgentToolsResponse is the response body for GET /v1/agents/{id}/tools.
// Emitted through the WriteSuccess envelope helper, so the final wire
// shape is {"data": {"tools": [...], "pagination": {...}}}.
type AgentToolsResponse struct {
	// Tools is the list of tools the agent is allowed to invoke.
	Tools      []AgentToolResponse      `json:"tools"`
	Pagination OffsetPaginationResponse `json:"pagination"`
}

// OffsetPaginationResponse provides offset-based pagination metadata.
type OffsetPaginationResponse struct {
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasNext bool `json:"has_next"`
}

// NewOffsetPaginationResponse builds the wire-format pagination metadata
// from the domain-layer OffsetPagination struct.
func NewOffsetPaginationResponse(p orchestrator.OffsetPagination) OffsetPaginationResponse {
	return OffsetPaginationResponse{
		Total:   p.Total,
		Limit:   p.Limit,
		Offset:  p.Offset,
		HasNext: p.HasNext,
	}
}

// ToAgentDetailResponse converts a domain AgentDetails to the API response format.
func ToAgentDetailResponse(d *orchestrator.AgentDetails) AgentDetailResponse {
	resp := AgentDetailResponse{
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
		// TODO(skills): populate from agent skills/capabilities when domain model exposes them
		Skills: []string{},
		Stats: AgentStatsResponse{
			TotalMessages:         d.Stats.TotalMessages,
			TotalTokensUsed:       d.Stats.TotalTokensUsed,
			TotalPromptTokens:     d.Stats.TotalPromptTokens,
			TotalCompletionTokens: d.Stats.TotalCompletionTokens,
			TotalRequests:         d.Stats.TotalRequests,
		},
	}
	if !d.UpdatedAt.IsZero() {
		t := d.UpdatedAt.Format(time.RFC3339)
		resp.UpdatedAt = &t
	}
	return resp
}

// ToAgentToolResponse converts a domain AgentTool to the API response format.
func ToAgentToolResponse(t orchestrator.AgentTool) AgentToolResponse {
	return AgentToolResponse{
		Name:        t.Name,
		DisplayName: t.DisplayName,
		Description: t.Description,
		Category: AgentToolCategoryResponse{
			ID:       t.Category.ID,
			Name:     t.Category.Name,
			ImageURL: t.Category.ImageURL,
		},
		IconURL: t.IconURL,
	}
}

// AgentMemoryResponse is the JSON response for a single memory entry.
type AgentMemoryResponse struct {
	Key       string `json:"key"`
	Content   string `json:"content"`
	Category  string `json:"category,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// AgentMemoriesListResponse is the paginated response for listing agent
// memories. Emitted through the WriteSuccess envelope helper, so the
// final wire shape is {"data": {"memories": [...], "pagination": {...}}}.
type AgentMemoriesListResponse struct {
	// Memories is the list of memory entries for the current page.
	Memories   []AgentMemoryResponse    `json:"memories"`
	Pagination OffsetPaginationResponse `json:"pagination"`
}

// CreateAgentMemoryRequest is the JSON request body for creating a memory.
type CreateAgentMemoryRequest struct {
	Key      string `json:"key"`
	Content  string `json:"content"`
	Category string `json:"category,omitempty"`
}
