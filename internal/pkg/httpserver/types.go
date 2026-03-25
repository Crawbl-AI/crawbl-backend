package httpserver

import orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"

const (
	AuthorizationHeader = "Authorization"
	XTokenHeader        = "X-Token"
	XTimestampHeader    = "X-Timestamp"
	XSignatureHeader    = "X-Signature"
	XDeviceInfoHeader   = "X-Device-Info"
	XDeviceIDHeader     = "X-Device-ID"
	XVersionHeader      = "X-Version"
	XTimezoneHeader     = "X-Timezone"
	ContentTypeJSON     = "application/json"
	BearerPrefix        = "Bearer "
	EnvironmentLocal    = "local"
)

type contextKey string

const (
	principalContextKey       contextKey = "principal"
	requestMetadataContextKey contextKey = "requestMetadata"
)

type AuthTokenSource string

const (
	AuthTokenSourceUnknown       AuthTokenSource = "unknown"
	AuthTokenSourceAuthorization AuthTokenSource = "authorization"
	AuthTokenSourceXToken        AuthTokenSource = "x-token"
)

type MiddlewareConfig struct {
	Environment      string
	HMACSecret       string
	IdentityVerifier orchestrator.IdentityVerifier
}

type RequestMetadata struct {
	TokenSource AuthTokenSource
	DeviceInfo  string
	DeviceID    string
	Version     string
	Timezone    string
}

type successResponseEnvelope struct {
	Data any `json:"data"`
}

type errorResponseEnvelope struct {
	Error string `json:"error"`
}
