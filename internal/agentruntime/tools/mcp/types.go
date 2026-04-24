package mcp

import (
	"errors"
	"net/http"
)

// conversationContextKey is the unexported context-value key used to carry
// the active conversation ID from the runtime's per-turn handler down
// through the ADK tool invocation into the MCP HTTP transport. A typed
// key avoids collisions with any other ctx values the ADK may install.
type conversationContextKey struct{}

// ErrMissingHMACConfig signals that a transport was constructed without
// the minimum required fields (signing key + user + workspace). Returning
// a typed error lets the caller fail fast during runtime bootstrap
// instead of getting opaque 401s from the orchestrator at first call time.
var ErrMissingHMACConfig = errors.New("mcp: HMAC transport requires signing key, user ID, and workspace ID")

// hmacRoundTripper wraps an underlying http.RoundTripper and stamps every
// outbound request with an "Authorization: Bearer <token>" header. The
// token is a fresh HMAC-signed (userID, workspaceID) pair per request,
// matching the scheme expected by internal/orchestrator/mcp/server.go:66.
type hmacRoundTripper struct {
	base        http.RoundTripper
	signingKey  string
	userID      string
	workspaceID string
}

// Closer is the shutdown hook returned alongside a Toolset. Phase 1
// implementation is a no-op because ADK mcptoolset does not expose an
// explicit session-close API — the session is torn down when the
// transport goes out of scope. This interface exists so US-AR-008's
// agent wiring and main.go's Shutdown can treat the MCP bridge like
// any other owned resource without caring about the underlying impl.
type Closer interface {
	Close() error
}

// nopCloser is the Phase 1 no-op Closer returned by Toolset. This is
// NOT interchangeable with io.NopCloser — io.NopCloser wraps an
// io.Reader and returns an io.ReadCloser; our Closer interface is a
// local one whose Close returns an error and takes no reader, so we
// need a dedicated zero-value type here.
type nopCloser struct{}

// Close is a no-op.
func (nopCloser) Close() error { return nil }
