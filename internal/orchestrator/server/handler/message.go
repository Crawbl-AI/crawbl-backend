package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httputil"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/convert"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
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

		var req mobilev1.ActionCardRequest
		if err := DecodeProtoJSON(r, &req); err != nil || req.GetActionId() == "" {
			httputil.WriteErrorMessage(w, http.StatusBadRequest, "action_id is required")
			return
		}

		msg, mErr := c.ChatService.RespondToActionCard(r.Context(), &orchestratorservice.RespondToActionCardOpts{
			UserID:      user.ID,
			WorkspaceID: workspaceID,
			MessageID:   messageID,
			ActionID:    req.GetActionId(),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteProtoSuccess(w, http.StatusOK, convert.MessageToProto(msg))
	}
}
