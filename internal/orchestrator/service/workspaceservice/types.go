package workspaceservice

import (
	"log/slog"

	workspacerepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/runtimeclient"
)

type workspaceRepo = workspacerepo.WorkspaceRepo

type service struct {
	workspaceRepo workspacerepo.WorkspaceRepo
	runtimeClient runtimeclient.Client
	logger        *slog.Logger
}
