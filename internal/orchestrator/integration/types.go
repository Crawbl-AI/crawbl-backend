// Package integration manages OAuth connections to third-party services.
//
// When a user connects a service (Slack, Gmail, Jira, etc.) through the
// mobile app's OAuth flow, the orchestrator stores the tokens here.
// MCP tool handlers use these tokens to make API calls on behalf of the user.
//
// Token lifecycle:
//  1. Mobile app initiates OAuth via /v1/integrations/{provider}/authorize
//  2. User completes OAuth in browser, callback hits /v1/integrations/callback
//  3. Orchestrator exchanges code for tokens and stores them encrypted
//  4. MCP tools call GetToken() which handles refresh automatically
//  5. User can revoke via /v1/integrations/{provider}/disconnect
//
// Security:
//   - Tokens are encrypted at rest using AES-256-GCM
//   - Encryption key is stored in AWS Secrets Manager, not in code
//   - Tokens are never exposed to agent runtime pods or MCP responses
//   - Each token is scoped to a single user+workspace+provider
package integration

// Connection status values.
const (
	StatusActive      = "active"
	StatusRevoked     = "revoked"
	StatusExpired     = "expired"
	StatusReauthorize = "reauthorize"
)
