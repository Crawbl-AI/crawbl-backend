// Package workspaceservice — ports.go declares the narrow repository
// contracts this package depends on. Per project convention, interfaces
// are defined at the consumer, not the producer.
package workspaceservice

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// workspaceStore is the workspace subset workspaceservice uses: list +
// get for the read endpoints and save for EnsureDefaultWorkspace.
type workspaceStore interface {
	ListByUserID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string) ([]*orchestrator.Workspace, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, workspace *orchestrator.Workspace) *merrors.Error
}
