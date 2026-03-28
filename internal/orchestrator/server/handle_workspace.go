package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// handleWorkspacesList retrieves all workspaces owned by the authenticated user.
// Each workspace includes its runtime status if available, allowing the mobile client
// to display workspace state and poll for readiness during initial provisioning.
func (s *Server) handleWorkspacesList(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromRequest(r)
	if err != nil {
		httpserver.WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
		return
	}

	user, mErr := s.authService.GetBySubject(r.Context(), &orchestratorservice.GetUserBySubjectOpts{
		Sess:    s.newSession(),
		Subject: principal.Subject,
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	workspaces, mErr := s.workspaceService.ListByUserID(r.Context(), &orchestratorservice.ListWorkspacesOpts{
		Sess:   s.newSession(),
		UserID: user.ID,
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	response := make([]workspaceResponse, 0, len(workspaces))
	for _, workspace := range workspaces {
		resp := toWorkspaceResponse(workspace)
		s.enrichWorkspaceResponse(r.Context(), workspace.ID, &resp)
		response = append(response, resp)
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, response)
}

// handleWorkspaceGet retrieves a single workspace by its ID.
// The workspace must be owned by the authenticated user.
// Returns workspace details including runtime status and verification state.
func (s *Server) handleWorkspaceGet(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	workspaceID := chi.URLParam(r, "id")
	workspace, mErr := s.workspaceService.GetByID(r.Context(), &orchestratorservice.GetWorkspaceOpts{
		Sess:        s.newSession(),
		UserID:      user.ID,
		WorkspaceID: workspaceID,
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	resp := toWorkspaceResponse(workspace)
	s.enrichWorkspaceResponse(r.Context(), workspace.ID, &resp)
	httpserver.WriteSuccessResponse(w, http.StatusOK, resp)
}

// toWorkspaceResponse converts a domain Workspace to the API response format.
// It includes the workspace runtime information if available, which indicates
// the swarm deployment status and verification state.
func toWorkspaceResponse(workspace *orchestrator.Workspace) workspaceResponse {
	response := workspaceResponse{
		ID:        workspace.ID,
		Name:      workspace.Name,
		CreatedAt: workspace.CreatedAt,
		UpdatedAt: workspace.UpdatedAt,
	}

	if workspace.Runtime != nil {
		response.Runtime = &workspaceRuntimeResponse{
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
func (s *Server) enrichWorkspaceResponse(ctx context.Context, workspaceID string, resp *workspaceResponse) {
	if resp.Runtime == nil {
		return
	}

	summary, mErr := s.chatService.GetWorkspaceSummary(ctx, &orchestratorservice.GetWorkspaceSummaryOpts{
		Sess:        s.newSession(),
		WorkspaceID: workspaceID,
	})
	if mErr != nil {
		return
	}

	resp.Runtime.TotalAgents = summary.TotalAgents
	if summary.LastMessagePreview != nil {
		resp.Runtime.LastMessagePreview = &lastMessagePreviewResponse{
			Text:       summary.LastMessagePreview.Text,
			SenderName: summary.LastMessagePreview.SenderName,
			Timestamp:  summary.LastMessagePreview.Timestamp,
		}
	}
}
