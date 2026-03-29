package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// AuditMiddleware logs every authenticated API request to the api_audit_logs table.
// Captures: user subject, HTTP method, path, status code, duration.
// Writes are async to avoid adding latency to responses.
func AuditMiddleware(db *dbr.Connection, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap the response writer to capture the status code.
			sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(sw, r)

			// Extract user from context (set by auth middleware).
			principal, ok := httpserver.PrincipalFromContext(r.Context())
			if !ok || principal == nil {
				return // unauthenticated request, skip audit
			}

			duration := time.Since(start)

			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				sess := db.NewSession(nil)
				_, err := sess.InsertInto("api_audit_logs").
					Pair("user_subject", principal.Subject).
					Pair("method", r.Method).
					Pair("path", r.URL.Path).
					Pair("status_code", sw.statusCode).
					Pair("duration_ms", int(duration.Milliseconds())).
					Pair("user_agent", r.UserAgent()).
					Pair("remote_addr", r.RemoteAddr).
					ExecContext(ctx)
				if err != nil {
					logger.Error("failed to write api audit log",
						slog.String("error", err.Error()),
						slog.String("path", r.URL.Path),
					)
				}
			}()
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the response status code.
type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}
