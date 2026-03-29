package integrationservice

import (
	"context"
	"log/slog"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// New creates a new IntegrationService.
func New(logger *slog.Logger) orchestratorservice.IntegrationService {
	if logger == nil {
		panic("integration service logger cannot be nil")
	}

	return &service{
		logger: logger,
	}
}

// providerCatalog defines all integrations the platform supports.
// Each entry becomes visible in the mobile app's Connected Apps screen.
// IsEnabled=false means "coming soon" (shown but not connectable).
var providerCatalog = []struct {
	Provider    string
	Name        string
	Description string
	IconURL     string
	IsEnabled   bool
}{
	{"google_calendar", "Google Calendar", "View and create calendar events, check availability", "https://cdn.crawbl.com/integrations/google-calendar.png", true},
	{"gmail", "Gmail", "Search and read emails, draft responses", "https://cdn.crawbl.com/integrations/gmail.png", true},
	{"slack", "Slack", "Send messages, search channels, manage notifications", "https://cdn.crawbl.com/integrations/slack.png", false},
	{"jira", "Jira", "Search and manage issues, track projects", "https://cdn.crawbl.com/integrations/jira.png", false},
	{"notion", "Notion", "Search pages, create documents, manage databases", "https://cdn.crawbl.com/integrations/notion.png", false},
	{"asana", "Asana", "Manage tasks, track project progress", "https://cdn.crawbl.com/integrations/asana.png", false},
	{"github", "GitHub", "Browse repositories, manage issues and pull requests", "https://cdn.crawbl.com/integrations/github.png", false},
	{"zoom", "Zoom", "Schedule and manage meetings", "https://cdn.crawbl.com/integrations/zoom.png", false},
}

// ListIntegrations returns all available integrations, merging the static catalog
// with the user's actual connection status from the database.
func (s *service) ListIntegrations(ctx context.Context, opts *orchestratorservice.ListIntegrationsOpts) ([]*orchestrator.IntegrationItem, *merrors.Error) {
	if opts == nil || opts.Sess == nil || opts.UserID == "" {
		return nil, merrors.ErrInvalidInput
	}

	// Query the user's active connections from the database.
	// If the table doesn't exist yet (migration not run), treat as no connections.
	connectedProviders := make(map[string]bool)
	type connRow struct {
		Provider string `db:"provider"`
	}
	var rows []connRow
	_, err := opts.Sess.Select("provider").
		From("integration_connections").
		Where("user_id = ? AND status = 'active'", opts.UserID).
		LoadContext(ctx, &rows)
	if err != nil {
		// Table may not exist yet — log and continue with empty connections.
		s.logger.Warn("failed to query integration_connections, assuming none connected",
			slog.String("error", err.Error()),
		)
	}
	for _, r := range rows {
		connectedProviders[r.Provider] = true
	}

	// Build the response by merging catalog with connection status.
	items := make([]*orchestrator.IntegrationItem, 0, len(providerCatalog))
	for _, p := range providerCatalog {
		items = append(items, &orchestrator.IntegrationItem{
			Provider:    p.Provider,
			Name:        p.Name,
			Description: p.Description,
			IconURL:     p.IconURL,
			IsConnected: connectedProviders[p.Provider],
			IsEnabled:   p.IsEnabled,
		})
	}

	return items, nil
}

// GetOAuthConfig returns the OAuth parameters for the given provider.
// The mobile app uses these to initiate the PKCE authorization flow.
func (s *service) GetOAuthConfig(ctx context.Context, opts *orchestratorservice.GetOAuthConfigOpts) (*orchestrator.OAuthConfig, *merrors.Error) {
	if opts == nil || opts.Provider == "" {
		return nil, merrors.ErrInvalidInput
	}

	// TODO: Load OAuth client credentials from config/secrets per provider.
	// For now, return not-configured error until OAuth clients are set up.
	s.logger.Info("OAuth connect requested (not yet configured)",
		slog.String("provider", opts.Provider),
		slog.String("user_id", opts.UserID),
	)

	return nil, merrors.NewBusinessError(
		"integration "+opts.Provider+" is not yet available — coming soon",
		merrors.ErrCodeIntegrationNotConfigured,
	)
}

// HandleOAuthCallback exchanges the authorization code for access/refresh tokens
// and stores the connection in the integration_connections table.
func (s *service) HandleOAuthCallback(ctx context.Context, opts *orchestratorservice.OAuthCallbackOpts) *merrors.Error {
	if opts == nil || opts.Provider == "" || opts.AuthorizationCode == "" {
		return merrors.ErrInvalidInput
	}

	// TODO: Exchange code for tokens via the provider's token endpoint.
	// Encrypt and store in integration_connections table.
	s.logger.Info("OAuth callback received (not yet implemented)",
		slog.String("provider", opts.Provider),
		slog.String("user_id", opts.UserID),
	)

	return merrors.NewBusinessError(
		"OAuth callback for "+opts.Provider+" is not yet implemented",
		merrors.ErrCodeIntegrationNotConfigured,
	)
}
