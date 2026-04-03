package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// GetAgent retrieves a single agent by ID.
// The agent must belong to a workspace owned by the authenticated user.
func GetAgent(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		agent, mErr := c.AgentService.GetAgent(r.Context(), &orchestratorservice.GetAgentOpts{
			Sess:    c.NewSession(),
			UserID:  user.ID,
			AgentID: chi.URLParam(r, "id"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, dto.ToAgentResponse(agent))
	}
}

// GetAgentDetails retrieves full agent profile including stats.
// The agent must belong to a workspace owned by the authenticated user.
func GetAgentDetails(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		details, mErr := c.AgentService.GetAgentDetails(r.Context(), &orchestratorservice.GetAgentDetailsOpts{
			Sess:    c.NewSession(),
			UserID:  user.ID,
			AgentID: chi.URLParam(r, "id"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, dto.ToAgentDetailResponse(details))
	}
}

// GetAgentHistory retrieves paginated conversation history for an agent.
func GetAgentHistory(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		limit := IntQueryParam(r, "limit")
		offset := IntQueryParam(r, "offset")

		items, pagination, mErr := c.AgentService.GetAgentHistory(r.Context(), &orchestratorservice.GetAgentHistoryOpts{
			Sess:    c.NewSession(),
			UserID:  user.ID,
			AgentID: chi.URLParam(r, "id"),
			Limit:   limit,
			Offset:  offset,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		historyItems := make([]dto.AgentHistoryItemResponse, 0, len(items))
		for _, item := range items {
			h := dto.AgentHistoryItemResponse{
				ConversationID: item.ConversationID,
				Title:          item.Title,
				Subtitle:       item.Subtitle,
			}
			if item.CreatedAt != nil {
				t := item.CreatedAt.Format("2006-01-02T15:04:05Z07:00")
				h.CreatedAt = &t
			}
			historyItems = append(historyItems, h)
		}

		WriteJSON(w, http.StatusOK, dto.AgentHistoryResponse{
			Items: historyItems,
			Pagination: dto.OffsetPaginationResponse{
				Total:   pagination.Total,
				Limit:   pagination.Limit,
				Offset:  pagination.Offset,
				HasNext: pagination.HasNext,
			},
		})
	}
}

// GetAgentSettings retrieves model and prompt settings for an agent.
func GetAgentSettings(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		settings, mErr := c.AgentService.GetAgentSettings(r.Context(), &orchestratorservice.GetAgentSettingsOpts{
			Sess:    c.NewSession(),
			UserID:  user.ID,
			AgentID: chi.URLParam(r, "id"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		prompts := make([]dto.AgentPromptResponse, 0, len(settings.Prompts))
		for _, p := range settings.Prompts {
			prompts = append(prompts, dto.AgentPromptResponse{
				ID:          p.ID,
				Name:        p.Name,
				Description: p.Description,
				Content:     p.Content,
			})
		}

		WriteSuccess(w, http.StatusOK, dto.AgentSettingsResponse{
			Model: dto.AgentModelResponse{
				ID:          settings.Model.ID,
				Name:        settings.Model.Name,
				Description: settings.Model.Description,
			},
			ResponseLength: string(settings.ResponseLength),
			Prompts:        prompts,
		})
	}
}

// GetAgentTools retrieves the tools assigned to an agent with offset pagination.
func GetAgentTools(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		limit := IntQueryParam(r, "limit")
		if limit == 0 {
			limit = 20
		}
		offset := IntQueryParam(r, "offset")

		page, mErr := c.AgentService.GetAgentTools(r.Context(), &orchestratorservice.GetAgentToolsOpts{
			Sess:    c.NewSession(),
			UserID:  user.ID,
			AgentID: chi.URLParam(r, "id"),
			Limit:   limit,
			Offset:  offset,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		tools := make([]dto.AgentToolResponse, 0, len(page.Data))
		for _, t := range page.Data {
			tools = append(tools, dto.ToAgentToolResponse(t))
		}

		WriteJSON(w, http.StatusOK, dto.AgentToolsResponse{
			Data: tools,
			Pagination: dto.OffsetPaginationResponse{
				Total:   page.Pagination.Total,
				Limit:   page.Pagination.Limit,
				Offset:  page.Pagination.Offset,
				HasNext: page.Pagination.HasNext,
			},
		})
	}
}

// ListModels returns the list of available LLM models.
// This is a public endpoint — no auth required (loaded by DictService before login).
func ListModels(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		models := make([]dto.AgentModelResponse, 0, len(orchestrator.AvailableModels))
		for _, m := range orchestrator.AvailableModels {
			models = append(models, dto.AgentModelResponse{
				ID:          m.ID,
				Name:        m.Name,
				Description: m.Description,
			})
		}

		WriteSuccess(w, http.StatusOK, models)
	}
}

// GetAgentMemories retrieves memories from the agent's ZeroClaw runtime.
func GetAgentMemories(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		category := r.URL.Query().Get("category")
		limit := IntQueryParam(r, "limit")
		if limit == 0 {
			limit = 20
		}
		offset := IntQueryParam(r, "offset")

		memories, mErr := c.AgentService.GetAgentMemories(r.Context(), &orchestratorservice.GetAgentMemoriesOpts{
			Sess:     c.NewSession(),
			UserID:   user.ID,
			AgentID:  chi.URLParam(r, "id"),
			Category: category,
			Limit:    limit,
			Offset:   offset,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		// Slice pagination over the full list from ZeroClaw.
		total := len(memories)
		start := offset
		if start > total {
			start = total
		}
		end := start + limit
		if end > total {
			end = total
		}
		page := memories[start:end]

		items := make([]dto.AgentMemoryResponse, 0, len(page))
		for _, m := range page {
			items = append(items, dto.AgentMemoryResponse{
				Key:       m.Key,
				Content:   m.Content,
				Category:  m.Category,
				CreatedAt: m.CreatedAt,
				UpdatedAt: m.UpdatedAt,
			})
		}

		WriteJSON(w, http.StatusOK, dto.AgentMemoriesListResponse{
			Data: items,
			Pagination: dto.OffsetPaginationResponse{
				Total:   total,
				Limit:   limit,
				Offset:  offset,
				HasNext: end < total,
			},
		})
	}
}

// DeleteAgentMemory removes a specific memory from the agent's ZeroClaw runtime.
func DeleteAgentMemory(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		mErr = c.AgentService.DeleteAgentMemory(r.Context(), &orchestratorservice.DeleteAgentMemoryOpts{
			Sess:    c.NewSession(),
			UserID:  user.ID,
			AgentID: chi.URLParam(r, "id"),
			Key:     chi.URLParam(r, "key"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// CreateAgentMemory stores a new memory in the agent's ZeroClaw runtime.
func CreateAgentMemory(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var body dto.CreateAgentMemoryRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			WriteError(w, merrors.ErrInvalidInput)
			return
		}

		mErr = c.AgentService.CreateAgentMemory(r.Context(), &orchestratorservice.CreateAgentMemoryOpts{
			Sess:     c.NewSession(),
			UserID:   user.ID,
			AgentID:  chi.URLParam(r, "id"),
			Key:      body.Key,
			Content:  body.Content,
			Category: body.Category,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
