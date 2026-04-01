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
	if sess == nil || strings.TrimSpace(agentID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row orchestratorrepo.AgentSettingsRow
	err := sess.Select(orchestratorrepo.Columns(settingsColumns...)...).
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

func (r *agentSettingsRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentSettingsRow) *merrors.Error {
	if sess == nil || row == nil {
		return merrors.ErrInvalidInput
	}

	var existing orchestratorrepo.AgentSettingsRow
	err := sess.Select(orchestratorrepo.Columns(settingsColumns...)...).
		From("agent_settings").
		Where("agent_id = ?", row.AgentID).
		LoadOneContext(ctx, &existing)
	switch {
	case err == nil:
		_, err = sess.Update("agent_settings").
			Set("model", row.Model).
			Set("response_length", row.ResponseLength).
			Set("allowed_tools", row.AllowedTools).
			Set("updated_at", row.UpdatedAt).
			Where("agent_id = ?", row.AgentID).
			ExecContext(ctx)
		if err != nil {
			return merrors.WrapStdServerError(err, "update agent settings")
		}
		return nil
	case !database.IsRecordNotFoundError(err):
		return merrors.WrapStdServerError(err, "select agent settings for save")
	}

	_, err = sess.InsertInto("agent_settings").
		Pair("agent_id", row.AgentID).
		Pair("model", row.Model).
		Pair("response_length", row.ResponseLength).
		Pair("allowed_tools", row.AllowedTools).
		Pair("created_at", row.CreatedAt).
		Pair("updated_at", row.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			_, err = sess.Update("agent_settings").
				Set("model", row.Model).
				Set("response_length", row.ResponseLength).
				Set("allowed_tools", row.AllowedTools).
				Set("updated_at", row.UpdatedAt).
				Where("agent_id = ?", row.AgentID).
				ExecContext(ctx)
			if err != nil {
				return merrors.WrapStdServerError(err, "update agent settings after duplicate insert")
			}
			return nil
		}
		return merrors.WrapStdServerError(err, "insert agent settings")
	}

	return nil
}
