package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// PanicRecoverer is middleware that recovers from panics and logs them with full context.
// It returns a 500 Internal Server Error to the client.
// Unlike chimiddleware.Recoverer, this logs the panic with stack trace for debugging.
func PanicRecoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					// Get request ID from chi middleware if available
					requestID := chimiddleware.GetReqID(r.Context())

					logger.Error("panic recovered",
						"method", r.Method,
						"path", r.URL.Path,
						"request_id", requestID,
						"panic", fmt.Sprintf("%v", rvr),
						"stack", string(debug.Stack()),
					)

					// Return generic error to client
					httpserver.WriteErrorResponse(w, http.StatusInternalServerError, "internal server error")
				}
			}()

			// Log incoming request at info level
			logger.Info("request started",
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", chimiddleware.GetReqID(r.Context()),
			)

			next.ServeHTTP(w, r)
		})
	}
}
