package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
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
func ListModels(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, mErr := c.CurrentUser(r); mErr != nil {
			WriteError(w, mErr)
			return
		}

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
