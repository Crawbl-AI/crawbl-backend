// Package integrationservice manages third-party OAuth connections: listing
// available integrations, returning OAuth config for the mobile app, and
// exchanging auth codes for tokens. Consumers depend on their own narrow
// interfaces (e.g. handler.integrationPort) rather than a producer-side
// contract.
package integrationservice

import (
	"context"
	"log/slog"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// Service implements integration (OAuth connection) operations.
// Consumers depend on their own consumer-side interfaces
// (e.g. handler.integrationPort) per the project's convention.
type Service struct {
	logger   *slog.Logger
	connRepo activeProviderLister
}

// activeProviderLister is the integration_connections subset this
// service uses: looking up active providers for a given user.
type activeProviderLister interface {
	ListActiveProviders(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, activeStatus string) ([]string, *merrors.Error)
}
