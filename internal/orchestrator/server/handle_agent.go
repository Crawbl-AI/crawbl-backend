package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// handleGetAgent retrieves a single agent by ID.
// The agent must belong to a workspace owned by the authenticated user.
func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	agent, mErr := s.agentService.GetAgent(r.Context(), &orchestratorservice.GetAgentOpts{
		Sess:    s.newSession(),
		UserID:  user.ID,
		AgentID: chi.URLParam(r, "id"),
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, toAgentResponse(agent))
}

// handleGetAgentDetails retrieves full agent profile including stats.
// The agent must belong to a workspace owned by the authenticated user.
func (s *Server) handleGetAgentDetails(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	details, mErr := s.agentService.GetAgentDetails(r.Context(), &orchestratorservice.GetAgentDetailsOpts{
		Sess:    s.newSession(),
		UserID:  user.ID,
		AgentID: chi.URLParam(r, "id"),
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, toAgentDetailResponse(details))
}

// agentHistoryResponse is the response envelope for GET /v1/agents/{id}/history.
type agentHistoryResponse struct {
	Items      []agentHistoryItemResponse `json:"items"`
	Pagination offsetPaginationResponse   `json:"pagination"`
}

// handleGetAgentHistory retrieves paginated conversation history for an agent.
func (s *Server) handleGetAgentHistory(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	limit := intQueryParam(r, "limit")
	offset := intQueryParam(r, "offset")

	items, pagination, mErr := s.agentService.GetAgentHistory(r.Context(), &orchestratorservice.GetAgentHistoryOpts{
		Sess:    s.newSession(),
		UserID:  user.ID,
		AgentID: chi.URLParam(r, "id"),
		Limit:   limit,
		Offset:  offset,
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	historyItems := make([]agentHistoryItemResponse, 0, len(items))
	for _, item := range items {
		h := agentHistoryItemResponse{
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

	httpserver.WriteJSONResponse(w, http.StatusOK, agentHistoryResponse{
		Items: historyItems,
		Pagination: offsetPaginationResponse{
			Total:   pagination.Total,
			Limit:   pagination.Limit,
			Offset:  pagination.Offset,
			HasNext: pagination.HasNext,
		},
	})
}

// handleGetAgentSettings retrieves model and prompt settings for an agent.
func (s *Server) handleGetAgentSettings(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	settings, mErr := s.agentService.GetAgentSettings(r.Context(), &orchestratorservice.GetAgentSettingsOpts{
		Sess:    s.newSession(),
		UserID:  user.ID,
		AgentID: chi.URLParam(r, "id"),
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	prompts := make([]agentPromptResponse, 0, len(settings.Prompts))
	for _, p := range settings.Prompts {
		prompts = append(prompts, agentPromptResponse{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			Content:     p.Content,
		})
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, agentSettingsResponse{
		Model: agentModelResponse{
			ID:          settings.Model.ID,
			Name:        settings.Model.Name,
			Description: settings.Model.Description,
		},
		ResponseLength: string(settings.ResponseLength),
		Prompts:        prompts,
	})
}

// agentToolsResponse is the response envelope for GET /v1/agents/{id}/tools.
type agentToolsResponse struct {
	Data       []agentToolResponse      `json:"data"`
	Pagination offsetPaginationResponse `json:"pagination"`
}

// handleAgentTools retrieves the tools assigned to an agent with offset pagination.
func (s *Server) handleAgentTools(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	limit := intQueryParam(r, "limit")
	if limit == 0 {
		limit = 20
	}
	offset := intQueryParam(r, "offset")

	page, mErr := s.agentService.GetAgentTools(r.Context(), &orchestratorservice.GetAgentToolsOpts{
		Sess:    s.newSession(),
		UserID:  user.ID,
		AgentID: chi.URLParam(r, "id"),
		Limit:   limit,
		Offset:  offset,
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	tools := make([]agentToolResponse, 0, len(page.Data))
	for _, t := range page.Data {
		tools = append(tools, toAgentToolResponse(t))
	}

	httpserver.WriteJSONResponse(w, http.StatusOK, agentToolsResponse{
		Data: tools,
		Pagination: offsetPaginationResponse{
			Total:   page.Pagination.Total,
			Limit:   page.Pagination.Limit,
			Offset:  page.Pagination.Offset,
			HasNext: page.Pagination.HasNext,
		},
	})
}

// handleListModels returns the list of available LLM models.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	models := make([]agentModelResponse, 0, len(orchestrator.AvailableModels))
	for _, m := range orchestrator.AvailableModels {
		models = append(models, agentModelResponse{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
		})
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, models)
}
