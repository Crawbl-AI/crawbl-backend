package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/middleware"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// WriteError writes a structured error response with the correct HTTP status.
func WriteError(w http.ResponseWriter, mErr *merrors.Error) {
	httpserver.WriteErrorResponse(w, HTTPStatusForError(mErr), mErr)
}

// WriteSuccess writes a success response wrapped in {"data": ...} envelope.
// If data implements proto.Message, it uses protojson for wire-compatible
// snake_case field naming. Otherwise falls back to encoding/json.
func WriteSuccess(w http.ResponseWriter, status int, data any) {
	if msg, ok := data.(proto.Message); ok {
		WriteProtoSuccess(w, status, msg)
		return
	}
	httpserver.WriteSuccessResponse(w, status, data)
}

// WriteJSON writes a JSON response without envelope wrapper.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	httpserver.WriteJSONResponse(w, status, payload)
}

// PrincipalFromRequest extracts the authenticated principal from request context.
func PrincipalFromRequest(r *http.Request) (*orchestrator.Principal, error) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		return nil, merrors.ErrUnauthorized
	}
	return principal, nil
}

// CurrentUser retrieves the full user from the DB using the principal in context.
// It rejects banned or soft-deleted users with a 403 Forbidden error so that
// every authenticated handler fails fast without per-handler checks.
func (c *Context) CurrentUser(r *http.Request) (*orchestrator.User, *merrors.Error) {
	principal, err := PrincipalFromRequest(r)
	if err != nil {
		return nil, merrors.ErrUnauthorized
	}
	user, mErr := c.AuthService.GetBySubject(r.Context(), &orchestratorservice.GetUserBySubjectOpts{
		Subject: principal.Subject,
	})
	if mErr != nil {
		return nil, mErr
	}
	if user.IsBanned {
		return nil, merrors.ErrUserBanned
	}
	if user.DeletedAt != nil {
		return nil, merrors.ErrUserDeleted
	}
	return user, nil
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

// IntQueryParam extracts a non-negative integer value from a query parameter.
// Returns 0 if the parameter is missing, cannot be parsed, or is negative.
func IntQueryParam(r *http.Request, key string) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

// Pagination extracts limit and offset query parameters from the request,
// applying sensible defaults (limit=20, offset=0) and clamping limit to
// a maximum of 100 so clients cannot request unbounded result sets.
func Pagination(r *http.Request) (limit, offset int) {
	limit = IntQueryParam(r, "limit")
	offset = IntQueryParam(r, "offset")
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
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
	case merrors.IsCode(err, merrors.ErrCodeNotImplemented):
		return http.StatusNotImplemented
	case merrors.IsCode(err, merrors.ErrCodeRuntimeNotReady):
		return http.StatusServiceUnavailable
	case merrors.IsCode(err, merrors.ErrCodeQuotaExceeded):
		return http.StatusTooManyRequests
	case merrors.IsCode(err, merrors.ErrCodeUserDeleted),
		merrors.IsCode(err, merrors.ErrCodeUserBanned):
		return http.StatusForbidden
	case merrors.IsBusinessError(err):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
