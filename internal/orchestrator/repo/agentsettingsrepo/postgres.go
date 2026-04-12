// Package agentsettingsrepo provides PostgreSQL-based implementation of the AgentSettingsRepo interface.
// It handles persistence and retrieval of per-agent settings.
package agentsettingsrepo

import (
	"context"
	"strings"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

func New() *agentSettingsRepo {
	return &agentSettingsRepo{}
}

func (r *agentSettingsRepo) GetByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*orchestratorrepo.AgentSettingsRow, *merrors.Error) {
	if strings.TrimSpace(agentID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row orchestratorrepo.AgentSettingsRow
	err := sess.Select(settingsColumns...).
		From("agent_settings").
		Where("agent_id = ?", agentID).
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, nil
		}
		return nil, merrors.WrapStdServerError(err, "select agent settings by agent id")
	}

	return &row, nil
}

// Save persists agent settings to the database.
// Returns ErrInvalidInput if sess is nil or row is nil.
// Raw SQL: dbr has no ON CONFLICT builder.
func (r *agentSettingsRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentSettingsRow) *merrors.Error {
	if row == nil {
		return merrors.ErrInvalidInput
	}

	const query = `
INSERT INTO agent_settings (agent_id, model, response_length, allowed_tools, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (agent_id) DO UPDATE SET
	model           = EXCLUDED.model,
	response_length = EXCLUDED.response_length,
	allowed_tools   = EXCLUDED.allowed_tools,
	updated_at      = EXCLUDED.updated_at`

	_, err := sess.InsertBySql(query,
		row.AgentID, row.Model, row.ResponseLength, row.AllowedTools, row.CreatedAt, row.UpdatedAt,
	).ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "upsert agent settings")
	}

	return nil
}
