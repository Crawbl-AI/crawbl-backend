// Package httputil provides orchestrator-agnostic HTTP helpers: success
// and error response envelope writers, generic header constants, shared
// content-type values, graceful server shutdown, and a minimal health
// check server.
package httputil

import (
	"log/slog"
	"net/http"
	"time"
)

// Generic HTTP header and content-type constants used by the response
// envelope helpers and by callers that need to set standard HTTP headers
// without pulling in orchestrator-specific middleware types.
//
// Orchestrator-specific header constants (X-Firebase-*, X-E2E-*, X-Device-*,
// X-Version, X-Timezone) and the authentication middleware itself live in
// internal/orchestrator/server/middleware/, which may legally import the
// orchestrator domain package. This package (internal/pkg/httputil) must
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

	// DefaultHealthPort is the default port for the health check server.
	DefaultHealthPort        = "7175"
	DefaultReadHeaderTimeout = 5 * time.Second
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

// HealthConfig holds health server configuration.
type HealthConfig struct {
	Port string
}

// HealthServer is a minimal HTTP server serving only /health.
type HealthServer struct {
	httpServer *http.Server
	logger     *slog.Logger
}
