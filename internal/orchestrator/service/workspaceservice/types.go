package workspaceservice

import (
	"log/slog"

	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

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
	// Typed against the consumer-side workspaceStore interface declared in
	// ports.go so the service does not import the producer interface.
	workspaceRepo workspaceStore

	// runtimeClient provides access to the agent runtime orchestration
	// layer for querying and ensuring runtime status for workspaces.
	runtimeClient userswarmclient.Client

	// logger provides structured logging for diagnostic output,
	// warnings, and error reporting within the service.
	logger *slog.Logger
}
