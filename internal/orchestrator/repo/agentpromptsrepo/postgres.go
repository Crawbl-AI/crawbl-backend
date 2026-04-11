package agentpromptsrepo

import (
	"context"
	"strings"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
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
	_, err := sess.Select(promptColumns...).
		From("agent_prompts").
		Where("agent_id = ?", agentID).
		OrderAsc("sort_order").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list agent prompts by agent id")
	}

	return rows, nil
}

// BulkSave upserts the provided agent prompt rows into the database.
// Each prompt is identified by its unique id.
// Raw SQL: dbr has no ON CONFLICT builder.
func (r *agentPromptsRepo) BulkSave(ctx context.Context, sess orchestratorrepo.SessionRunner, rows []orchestratorrepo.AgentPromptRow) *merrors.Error {
	if sess == nil {
		return merrors.ErrInvalidInput
	}

	const query = `
INSERT INTO agent_prompts (id, agent_id, name, description, content, sort_order, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	name        = EXCLUDED.name,
	description = EXCLUDED.description,
	content     = EXCLUDED.content,
	sort_order  = EXCLUDED.sort_order,
	updated_at  = EXCLUDED.updated_at`

	for i := range rows {
		row := &rows[i]
		_, err := sess.InsertBySql(query,
			row.ID, row.AgentID, row.Name, row.Description, row.Content,
			row.SortOrder, row.CreatedAt, row.UpdatedAt,
		).ExecContext(ctx)
		if err != nil {
			return merrors.WrapStdServerError(err, "upsert agent prompt")
		}
	}

	return nil
}
