package workspaceservice

import (
	"log/slog"

	workspacerepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/runtimeclient"
)

// workspaceRepo is a type alias for the WorkspaceRepo interface from the
// repository package. This alias provides a shorter, local name for the
// repository interface used by this service.
type workspaceRepo = workspacerepo.WorkspaceRepo

// service implements the WorkspaceService interface, providing workspace
// management capabilities backed by a repository and runtime client.
//
// This struct is the core implementation that coordinates between persistent
// storage (via workspaceRepo) and runtime orchestration (via runtimeClient).
// All public methods are defined on this struct to fulfill the WorkspaceService
// contract.
type service struct {
	// workspaceRepo provides access to workspace persistence operations
	// including listing, retrieval, and creation of workspace records.
	workspaceRepo workspacerepo.WorkspaceRepo

	// runtimeClient provides access to the swarm runtime orchestration
	// layer for querying and ensuring runtime status for workspaces.
	runtimeClient runtimeclient.Client

	// logger provides structured logging for diagnostic output,
	// warnings, and error reporting within the service.
	logger *slog.Logger
}
