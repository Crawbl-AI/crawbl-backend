package middleware

import (
	"net/http"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// SessionMiddleware creates a fresh *dbr.Session for every incoming HTTP
// request and stores it on the request context. Downstream handlers and
// services retrieve it via database.SessionFromContext(ctx).
func SessionMiddleware(db *dbr.Connection) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess := db.NewSession(nil)
			ctx := database.ContextWithSession(r.Context(), sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
