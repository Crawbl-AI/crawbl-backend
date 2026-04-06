// Package agenthistoryrepo provides PostgreSQL-based implementation of the AgentHistoryRepo interface.
// It handles persistence and retrieval of agent history entries.
package agenthistoryrepo

import (
	"context"
	"strings"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

func New() *agentHistoryRepo {
	return &agentHistoryRepo{}
}

func (r *agentHistoryRepo) ListByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string, limit, offset int) ([]orchestratorrepo.AgentHistoryRow, *merrors.Error) {
	if sess == nil || strings.TrimSpace(agentID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	query := sess.Select(orchestratorrepo.Columns(historyColumns...)...).
		From("agent_history").
		Where("agent_id = ?", agentID).
		OrderDesc("created_at")

	if limit > 0 {
		query = query.Limit(uint64(limit))
	}
	if offset > 0 {
		query = query.Offset(uint64(offset))
	}

	var rows []orchestratorrepo.AgentHistoryRow
	_, err := query.LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list agent history by agent id")
	}

	return rows, nil
}

func (r *agentHistoryRepo) CountByAgentID(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (int, *merrors.Error) {
	if sess == nil || strings.TrimSpace(agentID) == "" {
		return 0, merrors.ErrInvalidInput
	}

	var count int
	err := sess.Select("COUNT(*)").
		From("agent_history").
		Where("agent_id = ?", agentID).
		LoadOneContext(ctx, &count)
	if err != nil {
		return 0, merrors.WrapStdServerError(err, "count agent history by agent id")
	}

	return count, nil
}

func (r *agentHistoryRepo) Create(ctx context.Context, sess orchestratorrepo.SessionRunner, row *orchestratorrepo.AgentHistoryRow) *merrors.Error {
	if sess == nil || row == nil {
		return merrors.ErrInvalidInput
	}

	_, err := sess.InsertInto("agent_history").
		Pair("id", row.ID).
		Pair("agent_id", row.AgentID).
		Pair("conversation_id", row.ConversationID).
		Pair("title", row.Title).
		Pair("subtitle", row.Subtitle).
		Pair("created_at", row.CreatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			return nil
		}
		return merrors.WrapStdServerError(err, "insert agent history")
	}

	return nil
}
