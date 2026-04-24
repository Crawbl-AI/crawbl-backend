// Package httpserver provides orchestrator-agnostic HTTP helpers: success
// and error response envelope writers, generic header constants, and shared
// content-type values. It must not import internal/orchestrator — the
// orchestrator-specific auth middleware lives in
// internal/orchestrator/server/middleware/ precisely so this package can
// be reused by other binaries without transitively depending on the
// orchestrator domain.
package httpserver

// Generic HTTP header and content-type constants used by the response
// envelope helpers and by callers that need to set standard HTTP headers
// without pulling in orchestrator-specific middleware types.
//
// Orchestrator-specific header constants (X-Firebase-*, X-E2E-*, X-Device-*,
// X-Version, X-Timezone) and the authentication middleware itself live in
// internal/orchestrator/server/middleware/, which may legally import the
// orchestrator domain package. This package (internal/pkg/httpserver) must
// stay orchestrator-agnostic — see internal/pkg/AGENTS.md.
const (
	// AuthorizationHeader is the standard Authorization header.
	AuthorizationHeader = "Authorization"
	// XTokenHeader is the custom X-Token header for mobile authentication.
	XTokenHeader = "X-Token"
	// ContentTypeJSON is the JSON content type.
	ContentTypeJSON = "application/json"
	// BearerPrefix is the Bearer token prefix.
	BearerPrefix = "Bearer "

	headerContentType = "Content-Type"
)

// successResponseEnvelope wraps successful responses in a data envelope.
type successResponseEnvelope struct {
	Data any `json:"data"`
}

// errorResponseEnvelope wraps error responses in an error envelope.
// It matches the API contract: {"message": "string", "code": "string", "extra": {}}.
type errorResponseEnvelope struct {
	Message string         `json:"message"`
	Code    string         `json:"code,omitempty"`
	Extra   map[string]any `json:"extra,omitempty"`
}
