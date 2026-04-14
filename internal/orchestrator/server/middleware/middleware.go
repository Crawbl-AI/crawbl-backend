package middleware

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// maxRequestBodySize is the upper bound applied to every incoming request body.
// 1 MiB covers all current JSON endpoints (CreateAgentMemory, ConversationCreate, etc.)
// without allowing arbitrarily large payloads to exhaust server memory.
const maxRequestBodySize int64 = 1 << 20 // 1 MiB

// MaxBodyBytes wraps r.Body with http.MaxBytesReader so handlers decoding
// JSON cannot be crashed by unbounded payloads. A 1 MiB limit covers all
// current endpoints (CreateAgentMemory, CreateConversation, etc.).
func MaxBodyBytes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
		next.ServeHTTP(w, r)
	})
}

// Recoverer recovers from panics in downstream handlers and translates them
// into a structured 500 response that matches the API error envelope.
//
// Chi already ships github.com/go-chi/chi/v5/middleware.Recoverer, but it
// writes a plain-text body and logs with log.Print. We keep a custom
// implementation so the response uses the project's JSON error envelope
// (via httpserver.WriteErrorMessage) and the failure is emitted as a
// structured slog record with request_id, method, path, panic value, and
// full stack trace — which downstream log pipelines (Fluent Bit →
// VictoriaLogs) index for incident triage.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					requestID := chimiddleware.GetReqID(r.Context())
					logger.Error("panic recovered",
						"method", r.Method,
						"path", r.URL.Path,
						"request_id", requestID,
						"panic", fmt.Sprintf("%v", rvr),
						"stack", string(debug.Stack()),
					)
					httpserver.WriteErrorMessage(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogger logs every incoming HTTP request at Info level using slog.
//
// Chi's middleware.Logger writes with log.Printf-style output which does not
// integrate with the project's slog structured logging pipeline. We keep a
// tiny custom implementation so method, path, and request_id become
// structured fields rather than an opaque log line.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip logging for health probe endpoints to avoid log spam
			// (k8s probes hit these every 10s per pod).
			if r.URL.Path == "/v1/health" || r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			logger.Info("request started",
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", chimiddleware.GetReqID(r.Context()),
			)
			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddleware returns middleware that extracts user identity from
// Envoy Gateway-forwarded headers.
//
// In production, Envoy SecurityPolicy verifies Firebase JWTs and forwards
// claims as X-Firebase-UID/Email/Name headers. The middleware reads these
// trusted headers directly (Envoy strips them from external requests).
//
// In local/test environments, auth is skipped entirely.
func AuthMiddleware(cfg *MiddlewareConfig, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			env := strings.ToLower(strings.TrimSpace(cfg.Environment))

			if ctx, ok := localDevContext(r.Context(), env); ok {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if ctx, handled := applyE2EBypass(w, r, cfg, env, logger); handled {
				if ctx != nil {
					next.ServeHTTP(w, r.WithContext(ctx))
				}
				return
			}

			ctx, ok := applyFirebaseAuth(w, r, logger)
			if !ok {
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// envTest is the environment identifier for automated-test deployments.
const envTest = "test"

// envDev is the environment identifier for shared development clusters.
const envDev = "dev"

// localDevContext returns a context carrying the stubbed local principal when
// the environment is local or test, and reports whether the short-circuit
// applied. Production paths always take the caller-supplied auth flow.
func localDevContext(ctx context.Context, env string) (context.Context, bool) {
	if env != EnvironmentLocal && env != envTest {
		return nil, false
	}
	return ContextWithPrincipal(ctx, &orchestrator.Principal{
		Subject: "local-dev-user",
		Email:   "dev@crawbl.local",
		Name:    "Local Dev",
	}), true
}

// applyE2EBypass handles the CRAWBL_E2E_TOKEN auth-override flow used by the
// e2e suite. Returns the enriched context together with `handled=true` when
// the request matched the bypass path (either the enriched ctx or a nil ctx
// meaning the handler already wrote the response).
func applyE2EBypass(w http.ResponseWriter, r *http.Request, cfg *MiddlewareConfig, env string, logger *slog.Logger) (context.Context, bool) {
	if !e2eBypassAllowed(cfg, env) {
		return nil, false
	}
	token := strings.TrimSpace(r.Header.Get(XE2ETokenHeader))
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(cfg.E2EToken)) != 1 {
		return nil, false
	}
	e2eUID := strings.TrimSpace(r.Header.Get(XE2EUIDHeader))
	if e2eUID == "" {
		httpserver.WriteErrorMessage(w, http.StatusBadRequest, "X-E2E-UID header required with e2e token")
		return nil, true
	}

	principal := &orchestrator.Principal{
		Subject: e2eUID,
		Email:   strings.TrimSpace(r.Header.Get(XE2EEmailHeader)),
		Name:    strings.TrimSpace(r.Header.Get(XE2ENameHeader)),
	}
	logger.Info("e2e auth bypass",
		"method", r.Method,
		"path", r.URL.Path,
		"subject", principal.Subject,
	)

	ctx := ContextWithPrincipal(r.Context(), principal)
	ctx = ContextWithRequestMetadata(ctx, &RequestMetadata{
		TokenSource: AuthTokenSourceXToken,
		DeviceInfo:  "e2e-test",
		DeviceID:    "e2e-device",
		Version:     "e2e",
		Timezone:    "UTC",
	})
	return ctx, true
}

func e2eBypassAllowed(cfg *MiddlewareConfig, env string) bool {
	if cfg.E2EToken == "" {
		return false
	}
	return env == EnvironmentLocal || env == envDev
}

// applyFirebaseAuth reads the Envoy-forwarded Firebase claim headers and
// returns the enriched request context. On missing identity it writes a 401
// and returns ok=false so the caller short-circuits.
func applyFirebaseAuth(w http.ResponseWriter, r *http.Request, logger *slog.Logger) (context.Context, bool) {
	uid := strings.TrimSpace(r.Header.Get(XFirebaseUIDHeader))
	if uid == "" {
		httpserver.WriteErrorMessage(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}

	principal := &orchestrator.Principal{
		Subject: uid,
		Email:   strings.TrimSpace(r.Header.Get(XFirebaseEmailHeader)),
		Name:    strings.TrimSpace(r.Header.Get(XFirebaseNameHeader)),
	}

	logger.Info("auth middleware succeeded",
		"method", r.Method,
		"path", r.URL.Path,
		"subject", principal.Subject,
	)

	ctx := ContextWithPrincipal(r.Context(), principal)
	ctx = ContextWithRequestMetadata(ctx, &RequestMetadata{
		TokenSource: AuthTokenSourceXToken,
		DeviceInfo:  strings.TrimSpace(r.Header.Get(XDeviceInfoHeader)),
		DeviceID:    strings.TrimSpace(r.Header.Get(XDeviceIDHeader)),
		Version:     strings.TrimSpace(r.Header.Get(XVersionHeader)),
		Timezone:    strings.TrimSpace(r.Header.Get(XTimezoneHeader)),
	})
	return ctx, true
}
