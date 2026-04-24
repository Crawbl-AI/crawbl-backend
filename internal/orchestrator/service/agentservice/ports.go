// Package agentservice — ports.go declares the narrow repository contracts
// this package depends on. Per project convention, interfaces are defined
// at the consumer, not the producer.
package agentservice

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// workspaceGetter is the workspace subset agentservice uses: verifying the
// caller owns the workspace before returning agent-scoped data.
type workspaceGetter interface {
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
}

// agentStore is the agent subset agentservice uses: global lookup plus
// per-agent message counts for details endpoints.
type agentStore interface {
	GetByIDGlobal(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestrator.Agent, *merrors.Error)
	CountMessagesByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error)
}

// namedToolsGetter is the tools subset agentservice uses: resolving per-agent
// allow-listed tools for GetAgentTools.
type namedToolsGetter interface {
	GetByNames(ctx context.Context, sess orchestratorrepo.SessionRunner, names []string) ([]orchestrator.AgentTool, *merrors.Error)
}

// agentSettingsGetter is the settings subset agentservice reads for the
// agent details and tools endpoints.
type agentSettingsGetter interface {
	GetByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestratorrepo.AgentSettingsRow, *merrors.Error)
}

// agentPromptsLister is the prompt subset agentservice reads when
// assembling agent details responses.
type agentPromptsLister interface {
	ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) ([]orchestratorrepo.AgentPromptRow, *merrors.Error)
}

// agentHistoryStore is the history subset agentservice uses for the
// paginated history endpoint.
type agentHistoryStore interface {
	ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string, limit, offset int) ([]orchestratorrepo.AgentHistoryRow, *merrors.Error)
	CountByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error)
}

// drawerStore is the memory-drawer subset agentservice uses when the
// agent details endpoint surfaces memory entries.
type drawerStore interface {
	ListByWorkspace(ctx context.Context, sess database.SessionRunner, workspaceID string, limit, offset int) ([]memory.Drawer, error)
	Delete(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error
	Add(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error
}
