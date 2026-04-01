// Package agentservice provides the agent service implementation for handling
// agent details, settings, tools, and history retrieval. It enriches agent
// records with runtime status from the user swarm and verifies workspace
// ownership before returning data.
package agentservice

import (
	"context"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// service implements the orchestratorservice.AgentService interface.
type service struct {
	// workspaceRepo provides access to workspace data storage.
	workspaceRepo workspaceRepo
	// agentRepo provides access to agent data storage.
	agentRepo agentRepo
	// toolsRepo provides access to the tool catalog storage.
	toolsRepo toolsRepo
	// agentSettingsRepo provides access to agent settings storage.
	agentSettingsRepo agentSettingsRepo
	// agentPromptsRepo provides access to agent prompt storage.
	agentPromptsRepo agentPromptsRepo
	// agentHistoryRepo provides access to agent history storage.
	agentHistoryRepo agentHistoryRepo
	// runtimeClient communicates with the user swarm runtime for status enrichment.
	runtimeClient userswarmclient.Client
}

// workspaceRepo defines the repository interface for workspace data operations.
type workspaceRepo interface {
	// GetByID retrieves a workspace by its ID within the context of a specific user.
	GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
}

// agentRepo defines the repository interface for agent data operations.
type agentRepo interface {
	// GetByIDGlobal retrieves an agent by ID without workspace filtering.
	GetByIDGlobal(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestrator.Agent, *merrors.Error)
	// CountMessagesByAgentID counts the total number of messages attributed to an agent.
	CountMessagesByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error)
	// ListByWorkspaceID retrieves all agents belonging to a specific workspace.
	ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error)
	// Save persists an agent to storage with an optional sort order for consistent listing.
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, agent *orchestrator.Agent, sortOrder int) *merrors.Error
}

// toolsRepo defines the repository interface for tool catalog operations.
type toolsRepo interface {
	// List retrieves a paginated list of tools, optionally filtered by category.
	List(ctx context.Context, sess orchestratorrepo.SessionRunner, limit, offset int, category string) ([]orchestrator.AgentTool, *merrors.Error)
	// Count returns the total number of tools, optionally filtered by category.
	Count(ctx context.Context, sess orchestratorrepo.SessionRunner, category string) (int, *merrors.Error)
	// GetByNames retrieves tools matching the given name list.
	GetByNames(ctx context.Context, sess orchestratorrepo.SessionRunner, names []string) ([]orchestrator.AgentTool, *merrors.Error)
	// Seed inserts or updates the tool catalog from the canonical list.
	Seed(ctx context.Context, sess orchestratorrepo.SessionRunner, tools []orchestratorrepo.ToolRow) *merrors.Error
}

// agentSettingsRepo defines the repository interface for agent settings operations.
type agentSettingsRepo interface {
	// GetByAgentID retrieves settings for a specific agent.
	GetByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestratorrepo.AgentSettingsRow, *merrors.Error)
	// Save persists agent settings.
	Save(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentSettingsRow) *merrors.Error
}

// agentPromptsRepo defines the repository interface for agent prompt operations.
type agentPromptsRepo interface {
	// ListByAgentID retrieves all prompts for a specific agent.
	ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) ([]orchestratorrepo.AgentPromptRow, *merrors.Error)
	// BulkSave inserts multiple prompt rows in a single operation.
	BulkSave(ctx context.Context, sess orchestratorrepo.SessionRunner, rows []orchestratorrepo.AgentPromptRow) *merrors.Error
}

// agentHistoryRepo defines the repository interface for agent history operations.
type agentHistoryRepo interface {
	// ListByAgentID retrieves paginated history items for a specific agent.
	ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string, limit, offset int) ([]orchestratorrepo.AgentHistoryRow, *merrors.Error)
	// CountByAgentID returns the total number of history items for an agent.
	CountByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error)
}

