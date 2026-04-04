// Package workflowservice provides the workflow execution engine for the Crawbl
// multi-agent system. It manages workflow definitions, creates execution records,
// and runs steps sequentially by calling ZeroClaw agent runtimes.
package workflowservice

import (
	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// service implements the workflow execution engine.
type service struct {
	db            *dbr.Connection
	workflowRepo  workflowrepo.Repo
	runtimeClient userswarmclient.Client
	broadcaster   realtime.Broadcaster
}

// New creates a new workflow service with the provided dependencies.
func New(
	db *dbr.Connection,
	workflowRepo workflowrepo.Repo,
	runtimeClient userswarmclient.Client,
	broadcaster realtime.Broadcaster,
) *service {
	if db == nil {
		panic("workflow service db cannot be nil")
	}
	if workflowRepo == nil {
		panic("workflow service workflow repo cannot be nil")
	}
	if runtimeClient == nil {
		panic("workflow service runtime client cannot be nil")
	}
	if broadcaster == nil {
		broadcaster = realtime.NopBroadcaster{}
	}

	return &service{
		db:            db,
		workflowRepo:  workflowRepo,
		runtimeClient: runtimeClient,
		broadcaster:   broadcaster,
	}
}

// Repo returns the workflow repository for direct access by MCP tool handlers.
func (s *service) Repo() workflowrepo.Repo {
	return s.workflowRepo
}
