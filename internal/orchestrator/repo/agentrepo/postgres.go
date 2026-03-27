package agentrepo

import (
	"context"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// agentRepo is the PostgreSQL implementation of the AgentRepo interface.
// It handles agent data persistence and retrieval operations.
type agentRepo struct{}

// New creates a new AgentRepo instance backed by PostgreSQL.
// The returned repository uses the database session runner pattern for transaction support.
func New() *agentRepo {
	return &agentRepo{}
}

// ListByWorkspaceID retrieves all agents within a specific workspace.
// Results are ordered by sort order first, then by creation date for agents with the same sort order.
// Returns ErrInvalidInput if sess is nil or workspaceID is empty.
func (r *agentRepo) ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error) {
	if sess == nil || strings.TrimSpace(workspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []orchestratorrepo.AgentRow
	_, err := sess.Select(orchestratorrepo.Columns(agentColumns...)...).
		From("agents").
		Where("workspace_id = ?", workspaceID).
		OrderAsc("sort_order").
		OrderAsc("created_at").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list agents by workspace id")
	}

	agents := make([]*orchestrator.Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, row.ToDomain())
	}

	return agents, nil
}

// GetByID retrieves a specific agent by its ID, verifying workspace membership.
// Returns ErrAgentNotFound if the agent does not exist or does not belong to the specified workspace.
// Returns ErrInvalidInput if sess is nil, workspaceID is empty, or agentID is empty.
func (r *agentRepo) GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, agentID string) (*orchestrator.Agent, *merrors.Error) {
	if sess == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(agentID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row orchestratorrepo.AgentRow
	err := sess.Select(orchestratorrepo.Columns(agentColumns...)...).
		From("agents").
		Where("workspace_id = ? AND id = ?", workspaceID, agentID).
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrAgentNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select agent by id")
	}

	return row.ToDomain(), nil
}

// Save persists agent data to the database with a specified sort order.
// It handles both creating new agents and updating existing ones by checking
// if an agent with the same ID exists first.
// The sort order determines the display position of the agent within its workspace.
// Returns ErrInvalidInput if sess is nil or agent is nil.
func (r *agentRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, agent *orchestrator.Agent, sortOrder int) *merrors.Error {
	if sess == nil || agent == nil {
		return merrors.ErrInvalidInput
	}

	row := orchestratorrepo.NewAgentRow(agent, sortOrder)

	var existingRow orchestratorrepo.AgentRow
	err := sess.Select(orchestratorrepo.Columns(agentColumns...)...).
		From("agents").
		Where("id = ?", row.ID).
		LoadOneContext(ctx, &existingRow)
	switch {
	case err == nil:
		_, err = sess.Update("agents").
			Set("name", row.Name).
			Set("role", row.Role).
			Set("avatar_url", row.AvatarURL).
			Set("system_prompt", row.SystemPrompt).
			Set("sort_order", row.SortOrder).
			Set("updated_at", row.UpdatedAt).
			Where("id = ?", row.ID).
			ExecContext(ctx)
		if err != nil {
			return merrors.WrapStdServerError(err, "update agent")
		}
		return nil
	case !database.IsRecordNotFoundError(err):
		return merrors.WrapStdServerError(err, "select agent by id for save")
	}

	_, err = sess.InsertInto("agents").
		Pair("id", row.ID).
		Pair("workspace_id", row.WorkspaceID).
		Pair("name", row.Name).
		Pair("role", row.Role).
		Pair("avatar_url", row.AvatarURL).
		Pair("system_prompt", row.SystemPrompt).
		Pair("sort_order", row.SortOrder).
		Pair("created_at", row.CreatedAt).
		Pair("updated_at", row.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			_, err = sess.Update("agents").
				Set("name", row.Name).
				Set("role", row.Role).
				Set("avatar_url", row.AvatarURL).
				Set("sort_order", row.SortOrder).
				Set("updated_at", row.UpdatedAt).
				Where("id = ?", row.ID).
				ExecContext(ctx)
			if err != nil {
				return merrors.WrapStdServerError(err, "update agent after duplicate insert")
			}
			return nil
		}
		return merrors.WrapStdServerError(err, "insert agent")
	}

	return nil
}
