// Package handler provides HTTP handler functions for the orchestrator API.
// Each handler is a function that takes a *Context and returns an http.HandlerFunc,
// enabling dependency injection without receiver methods.
package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// Context holds shared dependencies for all handlers.
type Context struct {
	// DB is the database connection pool for all persistence operations.
	DB *dbr.Connection

	// Logger provides structured logging throughout the handler lifecycle.
	Logger *slog.Logger

	// AuthService handles user authentication, registration, and profile management.
	AuthService orchestratorservice.AuthService

	// WorkspaceService manages workspace provisioning and runtime state.
	WorkspaceService orchestratorservice.WorkspaceService

	// ChatService handles conversations, messages, and agent interactions.
	ChatService orchestratorservice.ChatService

	// AgentService handles agent details, settings, tools, and history retrieval.
	AgentService orchestratorservice.AgentService

	// IntegrationService manages third-party OAuth connections.
	IntegrationService orchestratorservice.IntegrationService

	// HTTPMiddleware contains authentication and request middleware configuration.
	HTTPMiddleware *httpserver.MiddlewareConfig

	// Broadcaster emits real-time events to connected WebSocket clients.
	Broadcaster realtime.Broadcaster

	// RuntimeClient manages agent runtime CRs for workspace provisioning and cleanup.
	RuntimeClient userswarmclient.Client
}

// NewSession creates a new database session.
func (c *Context) NewSession() *dbr.Session {
	return c.DB.NewSession(nil)
}

// WriteError writes a structured error response with the correct HTTP status.
func WriteError(w http.ResponseWriter, mErr *merrors.Error) {
	httpserver.WriteErrorResponse(w, HTTPStatusForError(mErr), merrors.PublicMessage(mErr))
}

// WriteSuccess writes a success response wrapped in {"data": ...} envelope.
func WriteSuccess(w http.ResponseWriter, status int, data any) {
	httpserver.WriteSuccessResponse(w, status, data)
}

// WriteJSON writes a JSON response without envelope wrapper.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	httpserver.WriteJSONResponse(w, status, payload)
}

// PrincipalFromRequest extracts the authenticated principal from request context.
func PrincipalFromRequest(r *http.Request) (*orchestrator.Principal, error) {
	principal, ok := httpserver.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		return nil, merrors.ErrUnauthorized
	}
	return principal, nil
}

// CurrentUser retrieves the full user from the DB using the principal in context.
func (c *Context) CurrentUser(r *http.Request) (*orchestrator.User, *merrors.Error) {
	principal, err := PrincipalFromRequest(r)
	if err != nil {
		return nil, merrors.ErrUnauthorized
	}
	return c.AuthService.GetBySubject(r.Context(), &orchestratorservice.GetUserBySubjectOpts{
		Sess:    c.NewSession(),
		Subject: principal.Subject,
	})
}

// DecodeJSON reads JSON from the request body into the target.
// It safely closes the request body after reading.
func DecodeJSON(r *http.Request, target any) error {
	if target == nil {
		return nil
	}
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(target)
}

// IntQueryParam extracts an integer value from a query parameter.
// Returns 0 if the parameter is missing or cannot be parsed as an integer.
func IntQueryParam(r *http.Request, key string) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0
	}
	var parsed int
	_, _ = fmt.Sscanf(raw, "%d", &parsed)
	return parsed
}

// StringOrEmpty safely dereferences a string pointer, returning an empty string
// if the pointer is nil. This prevents nil pointer dereference panics when
// converting optional string fields for API responses.
func StringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// HTTPStatusForError converts a domain error to the appropriate HTTP status code.
// Business errors map to 4xx codes, server errors map to 5xx.
func HTTPStatusForError(err *merrors.Error) int {
	switch {
	case err == nil:
		return http.StatusInternalServerError
	case merrors.IsCode(err, merrors.ErrCodeUnauthorized), merrors.IsCode(err, merrors.ErrCodeInvalidToken):
		return http.StatusUnauthorized
	case merrors.IsCode(err, merrors.ErrCodeAccountDeletionDisabled):
		return http.StatusForbidden
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
