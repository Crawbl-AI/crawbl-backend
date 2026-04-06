// Package integrationservice implements the IntegrationService interface.
// It manages third-party OAuth connections: listing available integrations,
// returning OAuth config for the mobile app, and exchanging auth codes for tokens.
package integrationservice

import (
	"log/slog"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
)

// service implements orchestratorservice.IntegrationService.
type service struct {
	logger   *slog.Logger
	connRepo orchestratorrepo.IntegrationConnRepo
}
