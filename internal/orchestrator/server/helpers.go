package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

func registerRoutes(s *Server) http.Handler {
	router := chi.NewRouter()
	router.Use(chimiddleware.RequestID)
	router.Use(chimiddleware.RealIP)
	router.Use(chimiddleware.Recoverer)

	router.Route("/v1", func(r chi.Router) {
		r.Get("/health", s.handleHealthCheck)
		r.Get("/legal", s.handleLegal)

		r.Group(func(r chi.Router) {
			r.Use(httpserver.AuthMiddleware(s.httpMiddleware))
			r.Post("/fcm-token", s.handleSaveFCMToken)
			r.Post("/auth/sign-in", s.handleAuthSignIn)
			r.Post("/auth/sign-up", s.handleAuthSignUp)
			r.Delete("/auth/delete", s.handleAuthDelete)
			r.Get("/users/profile", s.handleUsersProfile)
			r.Patch("/users", s.handleUsersUpdate)
			r.Get("/users/legal", s.handleUsersLegal)
			r.Post("/users/legal/accept", s.handleUsersLegalAccept)
			r.Get("/workspaces", s.handleWorkspacesList)
			r.Get("/workspaces/{id}", s.handleWorkspaceGet)
			r.Get("/workspaces/{workspaceId}/agents", s.handleWorkspaceAgentsList)
			r.Get("/workspaces/{workspaceId}/conversations", s.handleConversationsList)
			r.Get("/workspaces/{workspaceId}/conversations/{id}", s.handleConversationGet)
			r.Get("/workspaces/{workspaceId}/conversations/{id}/messages", s.handleMessagesList)
			r.Post("/workspaces/{workspaceId}/conversations/{id}/messages", s.handleMessagesSend)
		})
	})

	return router
}

func decodeJSON(r *http.Request, target any) error {
	if target == nil {
		return nil
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

func principalFromRequest(r *http.Request) (*orchestrator.Principal, error) {
	principal, ok := httpserver.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		return nil, orchestrator.ErrUnauthorized
	}
	return principal, nil
}

func (s *Server) currentUserFromRequest(r *http.Request) (*orchestrator.User, *merrors.Error) {
	principal, err := principalFromRequest(r)
	if err != nil {
		return nil, merrors.ErrUnauthorized
	}

	return s.authService.GetBySubject(r.Context(), &orchestratorservice.GetUserBySubjectOpts{
		Sess:    s.newSession(),
		Subject: principal.Subject,
	})
}

func (s *Server) newSession() *dbr.Session {
	return s.db.NewSession(nil)
}

func httpStatusForError(err *merrors.Error) int {
	switch {
	case err == nil:
		return http.StatusInternalServerError
	case merrors.IsCode(err, merrors.ErrCodeUnauthorized), merrors.IsCode(err, merrors.ErrCodeInvalidToken):
		return http.StatusUnauthorized
	case merrors.IsCode(err, merrors.ErrCodeUserNotFound),
		merrors.IsCode(err, merrors.ErrCodeWorkspaceNotFound),
		merrors.IsCode(err, merrors.ErrCodeAgentNotFound),
		merrors.IsCode(err, merrors.ErrCodeConversationNotFound),
		merrors.IsCode(err, merrors.ErrCodeMessageNotFound):
		return http.StatusNotFound
	case merrors.IsCode(err, merrors.ErrCodeRuntimeNotReady):
		return http.StatusServiceUnavailable
	case merrors.IsCode(err, merrors.ErrCodeUserDeleted):
		return http.StatusForbidden
	case merrors.IsBusinessError(err):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
