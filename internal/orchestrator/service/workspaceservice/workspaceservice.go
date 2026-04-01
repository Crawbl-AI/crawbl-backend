// Package workspaceservice provides the core implementation of WorkspaceService
// for managing user workspaces and their associated runtime status.
//
// This package handles workspace lifecycle operations including creation,
// retrieval, and listing of workspaces. It also integrates with the runtime
// client to attach runtime status information to workspace responses.
package workspaceservice

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// New creates a new WorkspaceService instance with the provided dependencies.
//
// The service requires a non-nil workspace repository for persistence operations,
// a non-nil runtime client for runtime status queries, and a non-nil logger
// for diagnostic output. If any dependency is nil, this function panics.
//
// Parameters:
//   - workspaceRepo: Repository interface for workspace CRUD operations.
//   - runtimeClient: Client interface for managing and querying swarm runtimes.
//   - logger: Structured logger for diagnostic and error logging.
//
// Returns an orchestratorservice.WorkspaceService implementation.
func New(workspaceRepo workspaceRepo, runtimeClient userswarmclient.Client, logger *slog.Logger) orchestratorservice.WorkspaceService {
	if workspaceRepo == nil {
		panic("workspace service repo cannot be nil")
	}
	if runtimeClient == nil {
		panic("workspace service runtime client cannot be nil")
	}
	if logger == nil {
		panic("workspace service logger cannot be nil")
	}

	return &service{
		workspaceRepo: workspaceRepo,
		runtimeClient: runtimeClient,
		logger:        logger,
	}
}

// EnsureDefaultWorkspace ensures that a user has at least one workspace.
//
// This method is idempotent and checks if the user already has workspaces
// before creating a new one. If the user has no workspaces, it creates
// a default workspace with the standard name defined in the orchestrator package.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - opts: Options containing session, user ID, and other required fields.
//
// Returns a merrors.Error if the input is invalid or if the repository
// operation fails. Returns nil on success or if a workspace already exists.
func (s *service) EnsureDefaultWorkspace(ctx context.Context, opts *orchestratorservice.EnsureDefaultWorkspaceOpts) *merrors.Error {
	if opts == nil || opts.Sess == nil || strings.TrimSpace(opts.UserID) == "" {
		return merrors.ErrInvalidInput
	}

	workspaces, mErr := s.workspaceRepo.ListByUserID(ctx, opts.Sess, opts.UserID)
	if mErr != nil {
		return mErr
	}
	if len(workspaces) > 0 {
		return nil
	}

	now := time.Now().UTC()
	workspace := &orchestrator.Workspace{
		ID:        uuid.NewString(),
		UserID:    opts.UserID,
		Name:      orchestrator.DefaultWorkspaceName,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if mErr := s.workspaceRepo.Save(ctx, opts.Sess, workspace); mErr != nil {
		return mErr
	}

	// Eagerly provision the UserSwarm runtime so agents are online by the
	// time the user reaches the workspace screen.
	if _, rErr := s.runtimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          opts.UserID,
		WorkspaceID:     workspace.ID,
		WaitForVerified: false,
	}); rErr != nil {
		s.logger.Warn("eager runtime provisioning failed",
			slog.String("workspace_id", workspace.ID),
			slog.String("user_id", opts.UserID),
			slog.String("error", rErr.Error()),
		)
	}

	return nil
}

// ListByUserID retrieves all workspaces for a given user with runtime status attached.
//
// This method fetches workspaces from the repository and enriches each workspace
// with its current runtime status by querying the runtime client. The runtime
// status includes information about the swarm phase, verification state, and
// any errors.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - opts: Options containing session and user ID for the query.
//
// Returns a slice of workspace pointers on success, or a merrors.Error
// if the input is invalid or the repository operation fails.
func (s *service) ListByUserID(ctx context.Context, opts *orchestratorservice.ListWorkspacesOpts) ([]*orchestrator.Workspace, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	workspaces, mErr := s.workspaceRepo.ListByUserID(ctx, opts.Sess, opts.UserID)
	if mErr != nil {
		return nil, mErr
	}

	for _, workspace := range workspaces {
		s.attachRuntimeStatus(ctx, workspace)
	}

	return workspaces, nil
}

// GetByID retrieves a single workspace by its ID with runtime status attached.
//
// This method fetches a specific workspace from the repository and enriches
// it with the current runtime status. The workspace must belong to the
// specified user.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - opts: Options containing session, user ID, and workspace ID.
//
// Returns the workspace pointer on success, or a merrors.Error if the input
// is invalid, the workspace is not found, or the repository operation fails.
func (s *service) GetByID(ctx context.Context, opts *orchestratorservice.GetWorkspaceOpts) (*orchestrator.Workspace, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	workspace, mErr := s.workspaceRepo.GetByID(ctx, opts.Sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	s.attachRuntimeStatus(ctx, workspace)
	return workspace, nil
}

// attachRuntimeStatus queries the runtime client for workspace runtime status
// and attaches it to the workspace.
//
// This method calls EnsureRuntime without waiting for verification to get
// the current runtime state. On success, it populates the workspace's Runtime
// field with phase, verification status, and resolved state. On failure,
// it sets the runtime status to an error state with the error message.
//
// If the workspace is nil, this method returns immediately without side effects.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - workspace: Pointer to the workspace to enrich with runtime status.
func (s *service) attachRuntimeStatus(ctx context.Context, workspace *orchestrator.Workspace) {
	if workspace == nil {
		return
	}

	runtimeStatus, mErr := s.runtimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          workspace.UserID,
		WorkspaceID:     workspace.ID,
		WaitForVerified: false,
	})
	if mErr != nil {
		s.logger.Warn("failed to ensure workspace runtime",
			slog.String("workspace_id", workspace.ID),
			slog.String("user_id", workspace.UserID),
			slog.String("error", mErr.Error()),
		)
		workspace.Runtime = &orchestrator.RuntimeStatus{
			Phase:     "Error",
			Verified:  false,
			Status:    orchestrator.RuntimeStateFailed,
			LastError: mErr.Error(),
		}
		return
	}

	if runtimeStatus.Status == "" {
		runtimeStatus.Status = orchestrator.ResolveRuntimeState(runtimeStatus.Phase, runtimeStatus.Verified)
	}
	workspace.Runtime = runtimeStatus
}
