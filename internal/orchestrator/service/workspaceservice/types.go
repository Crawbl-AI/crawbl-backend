package workspaceservice

import (
	"context"
	"log/slog"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// Service implements workspace management operations.
// Consumers depend on their own consumer-side interfaces
// (e.g. handler.workspacePort) per the project's "interfaces at
// consumer" convention.
type Service struct {
	// workspaceRepo provides access to workspace persistence operations
	// including listing, retrieval, and creation of workspace records.
	// Typed against the consumer-side workspaceStore interface declared in
	// types.go so the service does not import the producer interface.
	workspaceRepo workspaceStore

	// runtimeClient provides access to the agent runtime orchestration
	// layer for querying and ensuring runtime status for workspaces.
	runtimeClient userswarmclient.Client

	// logger provides structured logging for diagnostic output,
	// warnings, and error reporting within the service.
	logger *slog.Logger
}

// workspaceStore is the workspace subset workspaceservice uses: list +
// get for the read endpoints and save for EnsureDefaultWorkspace.
type workspaceStore interface {
	ListByUserID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID string) ([]*orchestrator.Workspace, *merrors.Error)
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, workspace *orchestrator.Workspace) *merrors.Error
}
