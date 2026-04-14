package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/proto"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/convert"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
)

// WorkspacesList retrieves all workspaces owned by the authenticated user.
// Each workspace includes its runtime status if available, allowing the mobile client
// to display workspace state and poll for readiness during initial provisioning.
func WorkspacesList(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		workspaces, mErr := c.WorkspaceService.ListByUserID(r.Context(), &orchestratorservice.ListWorkspacesOpts{
			UserID: user.ID,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		msgs := make([]proto.Message, 0, len(workspaces))
		for _, workspace := range workspaces {
			resp := convert.WorkspaceToProto(workspace)
			enrichWorkspaceProtoResponse(c, r.Context(), user.ID, workspace.ID, resp)
			msgs = append(msgs, resp)
		}
		WriteProtoArraySuccess(w, http.StatusOK, msgs)
	}
}

// WorkspaceGet retrieves a single workspace by its ID.
// The workspace must be owned by the authenticated user.
// Returns workspace details including runtime status and verification state.
func WorkspaceGet(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		workspaceID := chi.URLParam(r, "id")
		workspace, mErr := c.WorkspaceService.GetByID(r.Context(), &orchestratorservice.GetWorkspaceOpts{
			UserID:      user.ID,
			WorkspaceID: workspaceID,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		resp := convert.WorkspaceToProto(workspace)
		enrichWorkspaceProtoResponse(c, r.Context(), user.ID, workspace.ID, resp)
		WriteProtoSuccess(w, http.StatusOK, resp)
	}
}

// enrichWorkspaceProtoResponse fetches aggregate workspace data (agent count, last message)
// and attaches it to the runtime response. Errors are silently ignored since this
// data is supplementary and should not block the workspace response.
// userID is required for the defense-in-depth ownership check.
func enrichWorkspaceProtoResponse(c *Context, ctx context.Context, userID, workspaceID string, resp *mobilev1.WorkspaceResponse) {
	if resp.Runtime == nil {
		return
	}

	// Defense-in-depth: verify ownership before fetching summary data.
	// Callers already scope by user, but future callers may not.
	if _, mErr := c.WorkspaceService.GetByID(ctx, &orchestratorservice.GetWorkspaceOpts{
		UserID:      userID,
		WorkspaceID: workspaceID,
	}); mErr != nil {
		return
	}

	summary, mErr := c.ChatService.GetWorkspaceSummary(ctx, &orchestratorservice.GetWorkspaceSummaryOpts{
		WorkspaceID: workspaceID,
	})
	if mErr != nil {
		return
	}

	convert.EnrichWorkspaceRuntime(resp, summary)
}
