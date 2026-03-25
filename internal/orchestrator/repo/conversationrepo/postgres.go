package conversationrepo

import (
	"context"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

type conversationRepo struct{}

func New() *conversationRepo {
	return &conversationRepo{}
}

func (r *conversationRepo) ListByWorkspaceID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) ([]*orchestrator.Conversation, *merrors.Error) {
	if sess == nil || strings.TrimSpace(workspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []orchestratorrepo.ConversationRow
	_, err := sess.Select(orchestratorrepo.Columns(conversationColumns...)...).
		From("conversations").
		Where("workspace_id = ?", workspaceID).
		OrderDesc("updated_at").
		OrderDesc("created_at").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list conversations by workspace id")
	}

	conversations := make([]*orchestrator.Conversation, 0, len(rows))
	for _, row := range rows {
		conversations = append(conversations, row.ToDomain())
	}

	return conversations, nil
}

func (r *conversationRepo) GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) (*orchestrator.Conversation, *merrors.Error) {
	if sess == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(conversationID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row orchestratorrepo.ConversationRow
	err := sess.Select(orchestratorrepo.Columns(conversationColumns...)...).
		From("conversations").
		Where("workspace_id = ? AND id = ?", workspaceID, conversationID).
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrConversationNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select conversation by id")
	}

	return row.ToDomain(), nil
}

func (r *conversationRepo) FindDefaultSwarm(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) (*orchestrator.Conversation, *merrors.Error) {
	if sess == nil || strings.TrimSpace(workspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row orchestratorrepo.ConversationRow
	err := sess.Select(orchestratorrepo.Columns(conversationColumns...)...).
		From("conversations").
		Where("workspace_id = ? AND type = ?", workspaceID, orchestrator.ConversationTypeSwarm).
		OrderAsc("created_at").
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrConversationNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select default swarm conversation")
	}

	return row.ToDomain(), nil
}

func (r *conversationRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, conversation *orchestrator.Conversation) *merrors.Error {
	if sess == nil || conversation == nil {
		return merrors.ErrInvalidInput
	}

	row := orchestratorrepo.NewConversationRow(conversation)

	var existingRow orchestratorrepo.ConversationRow
	err := sess.Select(orchestratorrepo.Columns(conversationColumns...)...).
		From("conversations").
		Where("id = ?", row.ID).
		LoadOneContext(ctx, &existingRow)
	switch {
	case err == nil:
		_, err = sess.Update("conversations").
			Set("agent_id", row.AgentID).
			Set("type", row.Type).
			Set("title", row.Title).
			Set("unread_count", row.UnreadCount).
			Set("updated_at", row.UpdatedAt).
			Where("id = ?", row.ID).
			ExecContext(ctx)
		if err != nil {
			return merrors.WrapStdServerError(err, "update conversation")
		}
		return nil
	case !database.IsRecordNotFoundError(err):
		return merrors.WrapStdServerError(err, "select conversation by id for save")
	}

	_, err = sess.InsertInto("conversations").
		Pair("id", row.ID).
		Pair("workspace_id", row.WorkspaceID).
		Pair("agent_id", row.AgentID).
		Pair("type", row.Type).
		Pair("title", row.Title).
		Pair("unread_count", row.UnreadCount).
		Pair("created_at", row.CreatedAt).
		Pair("updated_at", row.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			_, err = sess.Update("conversations").
				Set("agent_id", row.AgentID).
				Set("type", row.Type).
				Set("title", row.Title).
				Set("unread_count", row.UnreadCount).
				Set("updated_at", row.UpdatedAt).
				Where("id = ?", row.ID).
				ExecContext(ctx)
			if err != nil {
				return merrors.WrapStdServerError(err, "update conversation after duplicate insert")
			}
			return nil
		}
		return merrors.WrapStdServerError(err, "insert conversation")
	}

	return nil
}
