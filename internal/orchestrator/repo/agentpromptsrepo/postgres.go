package agentpromptsrepo

import (
	"context"
	"strings"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

func New() *agentPromptsRepo {
	return &agentPromptsRepo{}
}

func (r *agentPromptsRepo) ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) ([]orchestratorrepo.AgentPromptRow, *merrors.Error) {
	if sess == nil || strings.TrimSpace(agentID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []orchestratorrepo.AgentPromptRow
	_, err := sess.Select(orchestratorrepo.Columns(promptColumns...)...).
		From("agent_prompts").
		Where("agent_id = ?", agentID).
		OrderAsc("sort_order").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list agent prompts by agent id")
	}

	return rows, nil
}

func (r *agentPromptsRepo) BulkSave(ctx context.Context, sess orchestratorrepo.SessionRunner, rows []orchestratorrepo.AgentPromptRow) *merrors.Error {
	if sess == nil {
		return merrors.ErrInvalidInput
	}

	for _, row := range rows {
		var existing orchestratorrepo.AgentPromptRow
		err := sess.Select(orchestratorrepo.Columns(promptColumns...)...).
			From("agent_prompts").
			Where("id = ?", row.ID).
			LoadOneContext(ctx, &existing)
		switch {
		case err == nil:
			_, err = sess.Update("agent_prompts").
				Set("name", row.Name).
				Set("description", row.Description).
				Set("content", row.Content).
				Set("sort_order", row.SortOrder).
				Set("updated_at", row.UpdatedAt).
				Where("id = ?", row.ID).
				ExecContext(ctx)
			if err != nil {
				return merrors.WrapStdServerError(err, "update agent prompt")
			}
		case database.IsRecordNotFoundError(err):
			_, err = sess.InsertInto("agent_prompts").
				Pair("id", row.ID).
				Pair("agent_id", row.AgentID).
				Pair("name", row.Name).
				Pair("description", row.Description).
				Pair("content", row.Content).
				Pair("sort_order", row.SortOrder).
				Pair("created_at", row.CreatedAt).
				Pair("updated_at", row.UpdatedAt).
				ExecContext(ctx)
			if err != nil {
				if database.IsRecordExistsError(err) {
					_, err = sess.Update("agent_prompts").
						Set("name", row.Name).
						Set("description", row.Description).
						Set("content", row.Content).
						Set("sort_order", row.SortOrder).
						Set("updated_at", row.UpdatedAt).
						Where("id = ?", row.ID).
						ExecContext(ctx)
					if err != nil {
						return merrors.WrapStdServerError(err, "update agent prompt after duplicate insert")
					}
					continue
				}
				return merrors.WrapStdServerError(err, "insert agent prompt")
			}
		default:
			return merrors.WrapStdServerError(err, "select agent prompt by id for save")
		}
	}

	return nil
}
