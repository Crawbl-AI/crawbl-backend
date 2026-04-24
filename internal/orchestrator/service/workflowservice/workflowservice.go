package workflowservice

import (
	"errors"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// New creates a new workflow service with the provided dependencies, returning
// an error if any required dependency is nil. A nil broadcaster is replaced
// with a no-op implementation.
func New(
	db *dbr.Connection,
	workflowRepo workflowrepo.Repo,
	runtimeClient userswarmclient.Client,
	broadcaster realtime.Broadcaster,
) (*service, error) {
	if db == nil {
		return nil, errors.New("workflowservice: db is required")
	}
	if workflowRepo == nil {
		return nil, errors.New("workflowservice: workflow repo is required")
	}
	if runtimeClient == nil {
		return nil, errors.New("workflowservice: runtime client is required")
	}
	if broadcaster == nil {
		broadcaster = realtime.NopBroadcaster{}
	}

	return &service{
		db:            db,
		workflowRepo:  workflowRepo,
		runtimeClient: runtimeClient,
		broadcaster:   broadcaster,
	}, nil
}

// MustNew wraps New and panics on dependency-validation errors. Intended for
// use from main/init paths where misconfiguration is unrecoverable.
func MustNew(
	db *dbr.Connection,
	workflowRepo workflowrepo.Repo,
	runtimeClient userswarmclient.Client,
	broadcaster realtime.Broadcaster,
) *service {
	svc, err := New(db, workflowRepo, runtimeClient, broadcaster)
	if err != nil {
		panic(err)
	}
	return svc
}

// Repo returns the workflow repository for direct access by MCP tool handlers.
func (s *service) Repo() workflowrepo.Repo {
	return s.workflowRepo
}
