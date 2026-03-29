package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// PanicRecoverer is middleware that recovers from panics and logs them with full context.
// It returns a 500 Internal Server Error to the client.
// Unlike chimiddleware.Recoverer, this logs the panic with stack trace for debugging.
func PanicRecoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					// Get request ID from chi middleware if available
					requestID := chimiddleware.GetReqID(r.Context())

					logger.Error("panic recovered",
						"method", r.Method,
						"path", r.URL.Path,
						"request_id", requestID,
						"panic", fmt.Sprintf("%v", rvr),
						"stack", string(debug.Stack()),
					)

					// Return generic error to client
					httpserver.WriteErrorResponse(w, http.StatusInternalServerError, "internal server error")
				}
			}()

			// Log incoming request at info level
			logger.Info("request started",
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", chimiddleware.GetReqID(r.Context()),
			)

			next.ServeHTTP(w, r)
		})
	}
}

// registerRoutes creates the HTTP router with all endpoints and middleware.
// It configures panic recovery, request ID, real IP, and authentication.
func registerRoutes(s *Server) http.Handler {
	router := chi.NewRouter()

	// Middleware stack (order matters):
	// 1. RequestID - generates unique ID for each request
	// 2. RealIP - extracts real client IP from headers
	// 3. PanicRecoverer - catches panics and logs them
	router.Use(chimiddleware.RequestID)
	router.Use(chimiddleware.RealIP)
	router.Use(PanicRecoverer(s.logger))

	router.Route("/v1", func(r chi.Router) {
		r.Get("/health", s.handleHealthCheck)
		r.Get("/legal", s.handleLegal)

		r.Group(func(r chi.Router) {
			r.Use(httpserver.AuthMiddleware(s.httpMiddleware, s.logger))
			r.Use(AuditMiddleware(s.db, s.logger))
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

// decodeJSON reads JSON from the request body into the target.
// It safely closes the request body after reading.
func decodeJSON(r *http.Request, target any) error {
	if target == nil {
		return nil
	}
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(target)
}

// principalFromRequest extracts the authenticated principal from the request context.
// Returns ErrUnauthorized if no principal is found.
func principalFromRequest(r *http.Request) (*orchestrator.Principal, error) {
	principal, ok := httpserver.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		return nil, orchestrator.ErrUnauthorized
	}
	return principal, nil
}

// currentUserFromRequest retrieves the current user from the database using the principal from context.
// Returns ErrUnauthorized if no principal is found, or the appropriate error if the user lookup fails.
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

// newSession creates a new database session for the current request.
func (s *Server) newSession() *dbr.Session {
	return s.db.NewSession(nil)
}

// httpStatusForError converts a domain error to the appropriate HTTP status code.
// Business errors map to 4xx codes, server errors map to 5xx.
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
