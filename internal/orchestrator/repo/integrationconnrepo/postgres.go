package integrationconnrepo

import (
	"context"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

type integrationConnRepo struct{}

func New() *integrationConnRepo {
	return &integrationConnRepo{}
}

// ListActiveProviders returns provider names with active connections
// for the given user.
// providerRow is the scan target for active-provider queries.
type providerRow struct {
	Provider string `db:"provider"`
}

func (r *integrationConnRepo) ListActiveProviders(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, activeStatus string) ([]string, *merrors.Error) {
	if sess == nil || userID == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []providerRow
	_, err := sess.Select("provider").
		From("integration_connections").
		Where("user_id = ? AND status = ?", userID, activeStatus).
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list active integration providers")
	}

	providers := make([]string, 0, len(rows))
	for _, r := range rows {
		providers = append(providers, r.Provider)
	}
	return providers, nil
}
