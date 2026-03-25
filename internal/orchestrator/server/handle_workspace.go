package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

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
		response = append(response, toWorkspaceResponse(workspace))
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, response)
}

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

	httpserver.WriteSuccessResponse(w, http.StatusOK, toWorkspaceResponse(workspace))
}

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
