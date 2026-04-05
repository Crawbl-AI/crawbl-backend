// Package hmac provides HMAC-SHA256 token generation and validation for
// internal service-to-service authentication.
//
// Tokens encode a key-value payload (e.g. userID:workspaceID) signed with
// a shared secret. This is used for:
//   - MCP bearer tokens (agent runtime → orchestrator)
//   - Any future internal auth that needs stateless, signed identity tokens
//
// Token format: base64url(payload).hmac_sha256_hex
//
// Usage:
//
//	token := hmac.GenerateToken(signingKey, "user-123", "ws-456")
//	id1, id2, err := hmac.ValidateToken(signingKey, token)
package hmac
