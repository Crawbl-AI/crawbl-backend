package agentrepo

import (
	"context"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// New creates a new AgentRepo instance backed by PostgreSQL.
// The returned repository uses the database session runner pattern for transaction support.
func New() *agentRepo {
	return &agentRepo{}
}

// ListByWorkspaceID retrieves all agents within a specific workspace.
// Results are ordered by sort order first, then by creation date for agents with the same sort order.
// Returns ErrInvalidInput if sess is nil or workspaceID is empty.
func (r *agentRepo) ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []orchestratorrepo.AgentRow
	_, err := sess.Select(agentColumns...).
		From("agents").
		Where("workspace_id = ?", workspaceID).
		OrderAsc("sort_order").
		OrderAsc("created_at").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list agents by workspace id")
	}

	agents := make([]*orchestrator.Agent, 0, len(rows))
	for i := range rows {
		agents = append(agents, rows[i].ToDomain())
	}

	return agents, nil
}

// loadAgentWhere loads a single agent row matching the given WHERE clause.
// Translates record-not-found into ErrAgentNotFound and wraps every other
// driver error with the caller-supplied operation label.
func loadAgentWhere(ctx context.Context, sess orchestratorrepo.SessionRunner, opLabel, whereClause string, whereArgs ...any) (*orchestrator.Agent, *merrors.Error) {
	var row orchestratorrepo.AgentRow
	err := sess.Select(agentColumns...).
		From("agents").
		Where(whereClause, whereArgs...).
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrAgentNotFound
		}
		return nil, merrors.WrapStdServerError(err, opLabel)
	}
	return row.ToDomain(), nil
}

// GetByID retrieves a specific agent by its ID, verifying workspace membership.
// Returns ErrAgentNotFound if the agent does not exist or does not belong to the specified workspace.
// Returns ErrInvalidInput if sess is nil, workspaceID is empty, or agentID is empty.
func (r *agentRepo) GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, agentID string) (*orchestrator.Agent, *merrors.Error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(agentID) == "" {
		return nil, merrors.ErrInvalidInput
	}
	return loadAgentWhere(ctx, sess, "select agent by id", "workspace_id = ? AND id = ?", workspaceID, agentID)
}

// Save persists agent data to the database with a specified sort order.
// The sort order determines the display position of the agent within its workspace.
// Returns ErrInvalidInput if sess is nil or agent is nil.
// Raw SQL: dbr has no ON CONFLICT builder.
func (r *agentRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, agent *orchestrator.Agent, sortOrder int) *merrors.Error {
	if agent == nil {
		return merrors.ErrInvalidInput
	}

	row := orchestratorrepo.NewAgentRow(agent, sortOrder)

	const query = `
INSERT INTO agents (
	id, workspace_id, name, role, slug, avatar_url,
	system_prompt, description, sort_order, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	name          = EXCLUDED.name,
	role          = EXCLUDED.role,
	slug          = EXCLUDED.slug,
	avatar_url    = EXCLUDED.avatar_url,
	system_prompt = EXCLUDED.system_prompt,
	description   = EXCLUDED.description,
	sort_order    = EXCLUDED.sort_order,
	updated_at    = EXCLUDED.updated_at`

	_, err := sess.InsertBySql(query,
		row.ID, row.WorkspaceID, row.Name, row.Role, row.Slug, row.AvatarURL,
		row.SystemPrompt, row.Description, row.SortOrder, row.CreatedAt, row.UpdatedAt,
	).ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "upsert agent")
	}

	return nil
}

// GetByIDGlobal retrieves a specific agent by its ID without workspace filtering.
// Returns ErrAgentNotFound if the agent does not exist.
// Returns ErrInvalidInput if sess is nil or agentID is empty.
func (r *agentRepo) GetByIDGlobal(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestrator.Agent, *merrors.Error) {
	if strings.TrimSpace(agentID) == "" {
		return nil, merrors.ErrInvalidInput
	}
	return loadAgentWhere(ctx, sess, "select agent by id global", "id = ?", agentID)
}

// GetBySlug retrieves a specific agent by its slug within a workspace.
// Returns ErrAgentNotFound if no agent with that slug exists in the workspace.
// Returns ErrInvalidInput if sess is nil, workspaceID is empty, or slug is empty.
func (r *agentRepo) GetBySlug(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, slug string) (*orchestrator.Agent, *merrors.Error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(slug) == "" {
		return nil, merrors.ErrInvalidInput
	}
	return loadAgentWhere(ctx, sess, "get agent by slug", "workspace_id = ? AND slug = ?", workspaceID, slug)
}

// CountMessagesByAgentID counts the total number of messages attributed to an agent.
// Returns ErrInvalidInput if sess is nil or agentID is empty.
func (r *agentRepo) CountMessagesByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error) {
	if strings.TrimSpace(agentID) == "" {
		return 0, merrors.ErrInvalidInput
	}

	var count int
	err := sess.Select("COUNT(*)").
		From("messages").
		Where("agent_id = ?", agentID).
		LoadOneContext(ctx, &count)
	if err != nil {
		return 0, merrors.WrapStdServerError(err, "count messages by agent id")
	}

	return count, nil
}
