// Package httpserver provides HTTP middleware and utilities for the backend.
// It includes authentication middleware, request context management, and response helpers.
package httpserver

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
)

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

