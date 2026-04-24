package database

import (
	"context"

	"github.com/gocraft/dbr/v2"
)

// ContextWithSession returns a derived context carrying the given database session.
// Used by the session middleware and by service code that needs to override the
// request session (e.g. per-goroutine sessions in parallel fan-out).
func ContextWithSession(ctx context.Context, sess *dbr.Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, sess)
}

// SessionFromContext extracts the per-request database session stored by the
// session middleware. Returns nil if no session is present (e.g. background
// jobs that don't go through HTTP).
func SessionFromContext(ctx context.Context) *dbr.Session {
	sess, _ := ctx.Value(sessionKey{}).(*dbr.Session)
	return sess
}
