package integrationservice

import (
	"context"
	"errors"
	"log/slog"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/integration"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/migrations/orchestrator/seed"
)

// New creates a new IntegrationService, returning an error if any required
// dependency is nil.
func New(logger *slog.Logger, connRepo integrationConnStore) (orchestratorservice.IntegrationService, error) {
	if logger == nil {
		return nil, errors.New("integrationservice: logger is required")
	}

	return &service{
		logger:   logger,
		connRepo: connRepo,
	}, nil
}

// MustNew wraps New and panics on dependency-validation errors. Intended for
// use from main/init paths where misconfiguration is unrecoverable.
func MustNew(logger *slog.Logger, connRepo integrationConnStore) orchestratorservice.IntegrationService {
	svc, err := New(logger, connRepo)
	if err != nil {
		panic(err)
	}
	return svc
}

// ListIntegrations returns all available integrations, merging the static catalog
// with the user's actual connection status from the database.
func (s *service) ListIntegrations(ctx context.Context, opts *orchestratorservice.ListIntegrationsOpts) ([]*orchestrator.IntegrationItem, *merrors.Error) {
	if opts == nil || opts.Sess == nil || opts.UserID == "" {
		return nil, merrors.ErrInvalidInput
	}

	// Query the user's active connections via the repo layer.
	// If the table doesn't exist yet (migration not run), treat as no connections.
	connectedProviders := make(map[string]bool)
	activeProviders, mErr := s.connRepo.ListActiveProviders(ctx, opts.Sess, opts.UserID, integration.StatusActive)
	if mErr != nil {
		s.logger.Warn("failed to query integration connections, assuming none connected",
			slog.String("error", mErr.Error()),
		)
	}
	for _, p := range activeProviders {
		connectedProviders[p] = true
	}

	// Build the response by merging seed catalog with connection status.
	providers := seed.IntegrationProviders()
	items := make([]*orchestrator.IntegrationItem, 0, len(providers))
	for _, p := range providers {
		items = append(items, &orchestrator.IntegrationItem{
			Provider:    p.Provider,
			Name:        p.Name,
			Description: p.Description,
			IconURL:     p.IconURL,
			CategoryID:  p.CategoryID,
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

	return nil, merrors.ErrIntegrationProviderNotSupported
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

	return merrors.ErrIntegrationCallbackFailed
}
