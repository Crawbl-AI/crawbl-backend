package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// WorkspacesList retrieves all workspaces owned by the authenticated user.
// Each workspace includes its runtime status if available, allowing the mobile client
// to display workspace state and poll for readiness during initial provisioning.
func WorkspacesList(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) ([]dto.WorkspaceResponse, *merrors.Error) {
		workspaces, mErr := c.WorkspaceService.ListByUserID(r.Context(), &orchestratorservice.ListWorkspacesOpts{
			UserID: deps.User.ID,
		})
		if mErr != nil {
			return nil, mErr
		}

		response := make([]dto.WorkspaceResponse, 0, len(workspaces))
		for _, workspace := range workspaces {
			resp := toWorkspaceResponse(workspace)
			enrichWorkspaceResponse(c, r.Context(), deps.User.ID, workspace.ID, &resp)
			response = append(response, resp)
		}

		return response, nil
	})
}

// WorkspaceGet retrieves a single workspace by its ID.
// The workspace must be owned by the authenticated user.
// Returns workspace details including runtime status and verification state.
func WorkspaceGet(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) (dto.WorkspaceResponse, *merrors.Error) {
		workspaceID := chi.URLParam(r, "id")
		workspace, mErr := c.WorkspaceService.GetByID(r.Context(), &orchestratorservice.GetWorkspaceOpts{
			UserID:      deps.User.ID,
			WorkspaceID: workspaceID,
		})
		if mErr != nil {
			return dto.WorkspaceResponse{}, mErr
		}

		resp := toWorkspaceResponse(workspace)
		enrichWorkspaceResponse(c, r.Context(), deps.User.ID, workspace.ID, &resp)
		return resp, nil
	})
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

	resp.Runtime.TotalAgents = summary.TotalAgents
	if summary.LastMessagePreview != nil {
		resp.Runtime.LastMessagePreview = &dto.LastMessagePreviewResponse{
			Text:       summary.LastMessagePreview.Text,
			SenderName: summary.LastMessagePreview.SenderName,
			Timestamp:  summary.LastMessagePreview.Timestamp,
		}
	}
}
