package workspaceservice

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/runtimeclient"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

func New(workspaceRepo workspaceRepo, runtimeClient runtimeclient.Client, logger *slog.Logger) orchestratorservice.WorkspaceService {
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
	return s.workspaceRepo.Save(ctx, opts.Sess, &orchestrator.Workspace{
		ID:        uuid.NewString(),
		UserID:    opts.UserID,
		Name:      orchestrator.DefaultWorkspaceName,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

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

func (s *service) attachRuntimeStatus(ctx context.Context, workspace *orchestrator.Workspace) {
	if workspace == nil {
		return
	}

	runtimeStatus, mErr := s.runtimeClient.EnsureRuntime(ctx, &runtimeclient.EnsureRuntimeOpts{
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
