package httpserver


// HTTP header constants used throughout the application.
const (
	// AuthorizationHeader is the standard Authorization header.
	AuthorizationHeader = "Authorization"
	// XTokenHeader is the custom X-Token header for mobile authentication.
	XTokenHeader = "X-Token"
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
	// ContentTypeJSON is the JSON content type.
	ContentTypeJSON = "application/json"
	// BearerPrefix is the Bearer token prefix.
	BearerPrefix = "Bearer "
	// EnvironmentLocal is the local development environment identifier.
	EnvironmentLocal = "local"
)

// contextKey is the type for context keys in this package.
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

// successResponseEnvelope wraps successful responses in a data envelope.
type successResponseEnvelope struct {
	Data any `json:"data"`
}

// errorResponseEnvelope wraps error responses in an error envelope.
type errorResponseEnvelope struct {
	Error string `json:"error"`
}
