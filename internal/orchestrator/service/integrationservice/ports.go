// Package integrationservice — ports.go declares the narrow repository
// contracts this package depends on. Per project convention, interfaces
// are defined at the consumer, not the producer.
package integrationservice

import (
	"context"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// integrationConnStore is the integration_connections subset this
// service uses: looking up active providers for a given user.
type integrationConnStore interface {
	ListActiveProviders(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, activeStatus string) ([]string, *merrors.Error)
}
