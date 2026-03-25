// Package httpserver provides HTTP middleware and utilities for the backend.
// It includes authentication middleware, request context management, and response helpers.
package httpserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

const (
	// maxTimestampAge is the maximum age of a timestamp before it's considered expired.
	maxTimestampAge = 2 * time.Minute
	// maxClockSkew allows for small time differences between client and server clocks.
	maxClockSkew = 30 * time.Second
)

// requiredDeviceHeaders lists all headers required for X-Token authentication.
// X-Token auth requires all of these headers for device validation.
var requiredDeviceHeaders = []string{
	XTimestampHeader,
	XSignatureHeader,
	XDeviceInfoHeader,
	XVersionHeader,
}

// AuthMiddleware returns middleware that validates authentication tokens.
// It supports two authentication methods:
//   - X-Token: Requires X-Timestamp, X-Signature, X-Device-Info, X-Version headers with HMAC signature
//   - Authorization: Bearer token (validated against the configured IdentityVerifier)
//
// On success, it injects the principal into the request context.
// On failure, it returns 401 Unauthorized with an appropriate error message.
func AuthMiddleware(cfg *MiddlewareConfig, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Recover from panics in auth middleware
			defer func() {
				if rvr := recover(); rvr != nil {
					logger.Error("panic in auth middleware",
						"method", r.Method,
						"path", r.URL.Path,
						"panic", fmt.Sprintf("%v", rvr),
						"stack", string(debug.Stack()),
					)
					WriteErrorResponse(w, http.StatusInternalServerError, "internal server error")
				}
			}()

			token, source := authTokenFromRequest(r)
			if token == "" {
				WriteErrorResponse(w, http.StatusUnauthorized,
					"missing authentication token: provide X-Token or Authorization header")
				return
			}

			logger.Info("auth middleware started",
				"method", r.Method,
				"path", r.URL.Path,
				"token_source", source,
			)

			if err := validateDeviceHeaders(cfg, r, source, token, logger); err != nil {
				WriteErrorResponse(w, http.StatusUnauthorized, err.Error())
				return
			}

			principal, err := cfg.IdentityVerifier.Verify(r.Context(), token)
			if err != nil {
				logger.Error("identity verification failed",
					"path", r.URL.Path,
					"method", r.Method,
					"error", err.Error())
				WriteErrorResponse(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			logger.Info("auth middleware succeeded",
				"method", r.Method,
				"path", r.URL.Path,
				"subject", principal.Subject,
			)

			ctx := ContextWithPrincipal(r.Context(), principal)
			ctx = ContextWithRequestMetadata(ctx, &RequestMetadata{
				TokenSource: source,
				DeviceInfo:  strings.TrimSpace(r.Header.Get(XDeviceInfoHeader)),
				DeviceID:    strings.TrimSpace(r.Header.Get(XDeviceIDHeader)),
				Version:     strings.TrimSpace(r.Header.Get(XVersionHeader)),
				Timezone:    strings.TrimSpace(r.Header.Get(XTimezoneHeader)),
			})

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// authTokenFromRequest extracts the authentication token from the request.
// It checks X-Token header first, then Authorization header.
// Returns the token and its source, or empty string with unknown source if not found.
func authTokenFromRequest(r *http.Request) (string, AuthTokenSource) {
	if token := strings.TrimSpace(r.Header.Get(XTokenHeader)); token != "" {
		return token, AuthTokenSourceXToken
	}

	authHeader := strings.TrimSpace(r.Header.Get(AuthorizationHeader))
	if authHeader == "" {
		return "", AuthTokenSourceUnknown
	}

	if strings.HasPrefix(authHeader, BearerPrefix) {
		if token := strings.TrimSpace(strings.TrimPrefix(authHeader, BearerPrefix)); token != "" {
			return token, AuthTokenSourceAuthorization
		}
	}

	return "", AuthTokenSourceUnknown
}

// validateDeviceHeaders validates the required device headers for X-Token authentication.
// It checks for missing headers, validates timestamp freshness, and verifies HMAC signature.
// Returns nil on success, or an error describing the validation failure.
//
//nolint:cyclop
func validateDeviceHeaders(cfg *MiddlewareConfig, r *http.Request, source AuthTokenSource, token string, logger *slog.Logger) error {
	if shouldSkipDeviceAuth(cfg) {
		return nil
	}

	if source != AuthTokenSourceXToken {
		return nil
	}

	hmacSecret := strings.TrimSpace(cfg.HMACSecret)
	if hmacSecret == "" {
		logger.Error("HMAC secret not configured")
		return fmt.Errorf("server misconfiguration")
	}

	// Collect required headers
	headers := make(map[string]string, len(requiredDeviceHeaders))
	for _, h := range requiredDeviceHeaders {
		headers[h] = strings.TrimSpace(r.Header.Get(h))
	}

	// Check for missing headers
	var missing []string
	for _, h := range requiredDeviceHeaders {
		if headers[h] == "" {
			missing = append(missing, h)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required headers: %s", strings.Join(missing, ", "))
	}

	// Validate timestamp
	timestamp := headers[XTimestampHeader]
	parsedTime, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return fmt.Errorf("invalid timestamp format: use RFC3339Nano")
	}

	now := time.Now().UTC()
	age := now.Sub(parsedTime)
	if age > maxTimestampAge || age < -maxClockSkew {
		return fmt.Errorf("timestamp expired")
	}

	// Validate signature
	signature := headers[XSignatureHeader]
	expectedSig := GenerateHMAC(hmacSecret, token+":"+timestamp)

	receivedMAC, err := hex.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("invalid signature format")
	}

	expectedMAC, err := hex.DecodeString(expectedSig)
	if err != nil {
		logger.Error("failed to decode expected signature", "error", err)
		return fmt.Errorf("internal error")
	}

	if !hmac.Equal(receivedMAC, expectedMAC) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// GenerateHMAC generates an HMAC-SHA256 signature for the given data using the secret.
func GenerateHMAC(secret, data string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// shouldSkipDeviceAuth returns true if device authentication should be skipped.
// Device auth is skipped in local and test environments.
func shouldSkipDeviceAuth(cfg *MiddlewareConfig) bool {
	if cfg == nil {
		return true
	}
	env := strings.ToLower(strings.TrimSpace(cfg.Environment))
	return env == EnvironmentLocal || env == "test"
}
