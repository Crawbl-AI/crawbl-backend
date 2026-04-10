package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// WorkspacesList retrieves all workspaces owned by the authenticated user.
// Each workspace includes its runtime status if available, allowing the mobile client
// to display workspace state and poll for readiness during initial provisioning.
func WorkspacesList(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := PrincipalFromRequest(r)
		if err != nil {
			httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
			return
		}

		user, mErr := c.AuthService.GetBySubject(r.Context(), &orchestratorservice.GetUserBySubjectOpts{
			Sess:    c.NewSession(),
			Subject: principal.Subject,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		workspaces, mErr := c.WorkspaceService.ListByUserID(r.Context(), &orchestratorservice.ListWorkspacesOpts{
			Sess:   c.NewSession(),
			UserID: user.ID,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		response := make([]dto.WorkspaceResponse, 0, len(workspaces))
		for _, workspace := range workspaces {
			resp := toWorkspaceResponse(workspace)
			enrichWorkspaceResponse(c, r.Context(), user.ID, workspace.ID, &resp)
			response = append(response, resp)
		}

		WriteSuccess(w, http.StatusOK, response)
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
			Sess:        c.NewSession(),
			UserID:      user.ID,
			WorkspaceID: workspaceID,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		resp := toWorkspaceResponse(workspace)
		enrichWorkspaceResponse(c, r.Context(), user.ID, workspace.ID, &resp)
		WriteSuccess(w, http.StatusOK, resp)
	}
}

// toWorkspaceResponse converts a domain Workspace to the API response format.
// It includes the workspace runtime information if available, which indicates
// the swarm deployment status and verification state.
func toWorkspaceResponse(workspace *orchestrator.Workspace) dto.WorkspaceResponse {
	response := dto.WorkspaceResponse{
		ID:        workspace.ID,
		Name:      workspace.Name,
		CreatedAt: workspace.CreatedAt,
		UpdatedAt: workspace.UpdatedAt,
	}

	if workspace.Runtime != nil {
		response.Runtime = &dto.WorkspaceRuntimeResponse{
			Status:   string(workspace.Runtime.Status),
			Phase:    workspace.Runtime.Phase,
			Verified: workspace.Runtime.Verified,
		}
	}

	return response
}

// enrichWorkspaceResponse fetches aggregate workspace data (agent count, last message)
// and attaches it to the runtime response. Errors are silently ignored since this
// data is supplementary and should not block the workspace response.
// userID is required for the defense-in-depth ownership check.
func enrichWorkspaceResponse(c *Context, ctx context.Context, userID, workspaceID string, resp *dto.WorkspaceResponse) {
	if resp.Runtime == nil {
		return
	}

	// Defense-in-depth: verify ownership before fetching summary data.
	// Callers already scope by user, but future callers may not.
	if _, mErr := c.WorkspaceService.GetByID(ctx, &orchestratorservice.GetWorkspaceOpts{
		Sess:        c.NewSession(),
		UserID:      userID,
		WorkspaceID: workspaceID,
	}); mErr != nil {
		return
	}

	summary, mErr := c.ChatService.GetWorkspaceSummary(ctx, &orchestratorservice.GetWorkspaceSummaryOpts{
		Sess:        c.NewSession(),
		WorkspaceID: workspaceID,
	})
	if mErr != nil {
		return
	}

	resp.Runtime.TotalAgents = summary.TotalAgents
	if summary.LastMessagePreview != nil {
		resp.Runtime.LastMessagePreview = &dto.LastMessagePreviewResponse{
			Text:       summary.LastMessagePreview.Text,
			SenderName: summary.LastMessagePreview.SenderName,
			Timestamp:  summary.LastMessagePreview.Timestamp,
		}
	}
}
