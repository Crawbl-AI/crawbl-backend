// Package middleware provides HTTP middleware for the orchestrator transport
// layer: authentication, panic recovery, request logging, body-size limits,
// and the context helpers used to stash the authenticated principal and
// request metadata on the per-request context.
//
// This package lives under internal/orchestrator/server/ because its types
// (MiddlewareConfig, RequestMetadata) and helpers (ContextWithPrincipal)
// reference the orchestrator domain. internal/pkg/httpserver must remain
// orchestrator-agnostic — putting this code here keeps the dependency
// direction one-way (server → service → repo) and satisfies the clean
// architecture rules in internal/pkg/AGENTS.md.
package middleware

// Header constants used by the authentication middleware. Envoy
// SecurityPolicy verifies the Firebase JWT and forwards the verified
// claims as X-Firebase-* headers; the middleware reads these trusted
// headers directly. Device/version headers come from the mobile client.
const (
	// XFirebaseUIDHeader is set by Envoy SecurityPolicy after JWT verification.
	XFirebaseUIDHeader = "X-Firebase-UID"
	// XFirebaseEmailHeader is set by Envoy SecurityPolicy after JWT verification.
	XFirebaseEmailHeader = "X-Firebase-Email"
	// XFirebaseNameHeader is set by Envoy SecurityPolicy after JWT verification.
	XFirebaseNameHeader = "X-Firebase-Name"
	// XDeviceInfoHeader contains device information.
	XDeviceInfoHeader = "X-Device-Info"
	// XDeviceIDHeader contains the unique device identifier.
	XDeviceIDHeader = "X-Device-ID"
	// XVersionHeader contains the client version.
	XVersionHeader = "X-Version"
	// XTimezoneHeader contains the client timezone.
	XTimezoneHeader = "X-Timezone"
	// EnvironmentLocal is the local development environment identifier.
	EnvironmentLocal = "local"
)

// E2E auth header constants.
const (
	// XE2ETokenHeader carries the shared secret for e2e test auth bypass.
	XE2ETokenHeader = "X-E2E-Token"
	// XE2EUIDHeader carries the test user's UID when using e2e auth.
	XE2EUIDHeader = "X-E2E-UID"
	// XE2EEmailHeader carries the test user's email when using e2e auth.
	XE2EEmailHeader = "X-E2E-Email"
	// XE2ENameHeader carries the test user's display name when using e2e auth.
	XE2ENameHeader = "X-E2E-Name"
)

// contextKey is the type for context keys in this package. Using a distinct
// type prevents collisions with context keys defined by other packages.
type contextKey string

// Context keys for storing request-scoped values.
const (
	// principalContextKey stores the authenticated principal.
	principalContextKey contextKey = "principal"
	// requestMetadataContextKey stores request metadata (device info, etc.).
	requestMetadataContextKey contextKey = "requestMetadata"
)

// AuthTokenSource identifies where the authentication token came from.
type AuthTokenSource string

// Auth token source constants.
const (
	// AuthTokenSourceUnknown indicates an unknown or missing token source.
	AuthTokenSourceUnknown AuthTokenSource = "unknown"
	// AuthTokenSourceAuthorization indicates the token came from the Authorization header.
	AuthTokenSourceAuthorization AuthTokenSource = "authorization"
	// AuthTokenSourceXToken indicates the token came from the X-Token header.
	AuthTokenSourceXToken AuthTokenSource = "x-token"
)

// String returns the string representation of the token source.
func (s AuthTokenSource) String() string {
	return string(s)
}

// MiddlewareConfig holds configuration for the authentication middleware.
type MiddlewareConfig struct {
	// Environment is the current environment (local, dev, prod, etc.).
	Environment string
	// E2EToken is a shared secret that enables e2e test auth bypass.
	// When set and a request carries a matching X-E2E-Token header,
	// the middleware reads identity from X-E2E-UID/Email/Name instead
	// of requiring a real Firebase JWT. Empty disables the bypass.
	E2EToken string
}

// RequestMetadata contains metadata extracted from request headers.
type RequestMetadata struct {
	// TokenSource indicates where the token originated.
	TokenSource AuthTokenSource
	// DeviceInfo contains device information from headers.
	DeviceInfo string
	// DeviceID is the unique device identifier.
	DeviceID string
	// Version is the client version.
	Version string
	// Timezone is the client timezone.
	Timezone string
}
