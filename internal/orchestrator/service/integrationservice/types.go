// Package integrationservice manages third-party OAuth connections: listing
// available integrations, returning OAuth config for the mobile app, and
// exchanging auth codes for tokens. Consumers depend on their own narrow
// interfaces (e.g. handler.integrationPort) rather than a producer-side
// contract.
package integrationservice

import (
	"log/slog"
)

// Service implements integration (OAuth connection) operations.
// Consumers depend on their own consumer-side interfaces
// (e.g. handler.integrationPort) per the project's convention.
type Service struct {
	logger   *slog.Logger
	connRepo activeProviderLister
}
