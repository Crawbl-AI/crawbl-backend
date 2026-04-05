package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// ActionCardResponse handles a user's response to an action card.
// POST /v1/workspaces/{workspaceId}/messages/{id}/action
func ActionCardResponse(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		workspaceID := chi.URLParam(r, "workspaceId")
		messageID := chi.URLParam(r, "id")

		var req dto.ActionCardRequest
		if err := DecodeJSON(r, &req); err != nil || req.ActionID == "" {
			httpserver.WriteErrorResponse(w, http.StatusBadRequest, "action_id is required")
			return
		}

		msg, mErr := c.ChatService.RespondToActionCard(r.Context(), &orchestratorservice.RespondToActionCardOpts{
			Sess:        c.NewSession(),
			UserID:      user.ID,
			WorkspaceID: workspaceID,
			MessageID:   messageID,
			ActionID:    req.ActionID,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, dto.ToMessageResponse(msg))
	}
}
