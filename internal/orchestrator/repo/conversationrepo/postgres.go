package conversationrepo

import (
	"context"
	"strings"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// New creates a new ConversationRepo instance backed by PostgreSQL.
// The returned repository uses the database session runner pattern for transaction support.
func New() *conversationRepo {
	return &conversationRepo{}
}

// ListByWorkspaceID retrieves all conversations within a specific workspace.
// Results are ordered by updated_at descending (most recently updated first),
// then by created_at descending as a secondary sort.
// Returns ErrInvalidInput if sess is nil or workspaceID is empty.
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

// GetByID retrieves a specific conversation by its ID, verifying workspace membership.
// Returns ErrConversationNotFound if the conversation does not exist or does not belong
// to the specified workspace.
// Returns ErrInvalidInput if sess is nil, workspaceID is empty, or conversationID is empty.
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

// FindDefaultSwarm retrieves the default swarm conversation for a workspace.
// A swarm conversation is the primary conversation type where all agents in a workspace
// collaborate together. The default swarm is the first (oldest) swarm conversation created.
// Returns ErrConversationNotFound if no swarm conversation exists for the workspace.
// Returns ErrInvalidInput if sess is nil or workspaceID is empty.
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

// Save persists conversation data to the database.
// It handles both creating new conversations and updating existing ones by checking
// if a conversation with the same ID exists first.
// Returns ErrInvalidInput if sess is nil or conversation is nil.
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
