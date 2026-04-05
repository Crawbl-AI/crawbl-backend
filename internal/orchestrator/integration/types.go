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

import (
	"time"
)

// Supported OAuth providers.
const (
	ProviderSlack    = "slack"
	ProviderGmail    = "gmail"
	ProviderCalendar = "google_calendar"
	ProviderJira     = "jira"
	ProviderAsana    = "asana"
	ProviderNotion   = "notion"
	ProviderZoom     = "zoom"
	ProviderGithub   = "github"
)

// Connection status values.
const (
	StatusActive      = "active"
	StatusRevoked     = "revoked"
	StatusExpired     = "expired"
	StatusReauthorize = "reauthorize"
)

// Connection represents an OAuth connection to a third-party service.
type Connection struct {
	ID                    string
	UserID                string
	WorkspaceID           string
	Provider              string
	Status                string
	AccessTokenEncrypted  string
	RefreshTokenEncrypted string
	TokenExpiresAt        *time.Time
	Scopes                []string
	ProviderUserID        string
	ProviderUserEmail     string
	Metadata              map[string]any
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// ConnectionRow is the database row representation.
type ConnectionRow struct {
	ID                    string     `db:"id"`
	UserID                string     `db:"user_id"`
	WorkspaceID           string     `db:"workspace_id"`
	Provider              string     `db:"provider"`
	Status                string     `db:"status"`
	AccessTokenEncrypted  *string    `db:"access_token_encrypted"`
	RefreshTokenEncrypted *string    `db:"refresh_token_encrypted"`
	TokenExpiresAt        *time.Time `db:"token_expires_at"`
	Scopes                []string   `db:"scopes"`
	ProviderUserID        *string    `db:"provider_user_id"`
	ProviderUserEmail     *string    `db:"provider_user_email"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
}

// OAuthConfig holds the OAuth client credentials for a provider.
type OAuthConfig struct {
	Provider     string
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	RedirectURL  string
	Scopes       []string
}
