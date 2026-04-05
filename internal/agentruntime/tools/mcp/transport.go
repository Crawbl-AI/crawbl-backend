// Package mcp bridges orchestrator-mediated tools into the
// crawbl-agent-runtime. The runtime never implements orchestrator__*
// tools locally; instead, it opens an MCP client session against the
// orchestrator's `/mcp/v1` streamable HTTP endpoint and forwards every
// call with a per-request HMAC bearer token.
//
// This file owns the HTTP transport piece — specifically, the
// net/http.RoundTripper that signs every outbound request with an
// HMAC bearer token derived from (userID, workspaceID) using
// internal/pkg/hmac. The MCP session client (client.go) plugs this
// transport into the upstream StreamableClientTransport so the MCP
// SDK's authoritative request/response wire handling stays untouched.
//
// Design rules:
//   - Tokens are regenerated on every request, not cached. HMAC
//     generation is cheap and there is no server-side revocation list,
//     so caching adds complexity without benefit.
//   - The transport NEVER logs the token, even at debug level.
//   - Base RoundTripper defaults to http.DefaultTransport when nil, so
//     constructors can pass nil for typical use.
package mcp

import (
	"errors"
	"net/http"

	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

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

// newHMACTransport builds a signed-request RoundTripper ready to feed into
// the MCP StreamableClientTransport. Validates the required fields so a
// misconfigured runtime fails at startup, not on first tool call.
func newHMACTransport(base http.RoundTripper, signingKey, userID, workspaceID string) (http.RoundTripper, error) {
	if signingKey == "" || userID == "" || workspaceID == "" {
		return nil, ErrMissingHMACConfig
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &hmacRoundTripper{
		base:        base,
		signingKey:  signingKey,
		userID:      userID,
		workspaceID: workspaceID,
	}, nil
}

// RoundTrip signs the request with a fresh HMAC bearer token and delegates
// to the base transport. The request is cloned before mutation so the
// caller's original request object is never modified — this matters
// because net/http's retry logic may re-use the original.
func (t *hmacRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	token := crawblhmac.GenerateToken(t.signingKey, t.userID, t.workspaceID)
	cloned := req.Clone(req.Context())
	if cloned.Header == nil {
		cloned.Header = make(http.Header)
	}
	cloned.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(cloned)
}

// newSignedHTTPClient builds a ready-to-use *http.Client with the HMAC
// round-tripper installed. The MCP StreamableClientTransport accepts an
// *http.Client directly, so this is the shape callers need.
func newSignedHTTPClient(signingKey, userID, workspaceID string) (*http.Client, error) {
	rt, err := newHMACTransport(nil, signingKey, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: rt}, nil
}
