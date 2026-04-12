package conversationrepo

import (
	"context"
	"strings"
	"time"

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
	if strings.TrimSpace(workspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var rows []orchestratorrepo.ConversationRow
	_, err := sess.Select(conversationColumns...).
		From("conversations").
		Where("workspace_id = ?", workspaceID).
		OrderDesc("updated_at").
		OrderDesc("created_at").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list conversations by workspace id")
	}

	conversations := make([]*orchestrator.Conversation, 0, len(rows))
	for i := range rows {
		conversations = append(conversations, rows[i].ToDomain())
	}

	return conversations, nil
}

// GetByID retrieves a specific conversation by its ID, verifying workspace membership.
// Returns ErrConversationNotFound if the conversation does not exist or does not belong
// to the specified workspace.
// Returns ErrInvalidInput if sess is nil, workspaceID is empty, or conversationID is empty.
func (r *conversationRepo) GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) (*orchestrator.Conversation, *merrors.Error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(conversationID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row orchestratorrepo.ConversationRow
	err := sess.Select(conversationColumns...).
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
	if strings.TrimSpace(workspaceID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row orchestratorrepo.ConversationRow
	err := sess.Select(conversationColumns...).
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

// Create inserts a new conversation row into the database.
// Returns ErrInvalidInput if sess is nil or conversation is nil.
// Returns a server error if the insert fails.
func (r *conversationRepo) Create(ctx context.Context, sess orchestratorrepo.SessionRunner, conversation *orchestrator.Conversation) *merrors.Error {
	if conversation == nil {
		return merrors.ErrInvalidInput
	}

	row := orchestratorrepo.NewConversationRow(conversation)

	_, err := sess.InsertInto("conversations").
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
		return merrors.WrapStdServerError(err, "insert conversation")
	}

	return nil
}

// Delete removes a conversation from the database by workspace ID and conversation ID.
// Returns ErrInvalidInput if sess is nil, workspaceID is empty, or conversationID is empty.
// Returns ErrConversationNotFound if no matching row exists.
func (r *conversationRepo) Delete(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) *merrors.Error {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(conversationID) == "" {
		return merrors.ErrInvalidInput
	}

	result, err := sess.DeleteFrom("conversations").
		Where("workspace_id = ? AND id = ?", workspaceID, conversationID).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "delete conversation")
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return merrors.ErrConversationNotFound
	}

	return nil
}

// Save persists conversation data to the database.
// Returns ErrInvalidInput if sess is nil or conversation is nil.
// Raw SQL: dbr has no ON CONFLICT builder.
func (r *conversationRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, conversation *orchestrator.Conversation) *merrors.Error {
	if conversation == nil {
		return merrors.ErrInvalidInput
	}

	row := orchestratorrepo.NewConversationRow(conversation)

	const query = `
INSERT INTO conversations (
	id, workspace_id, agent_id, type, title, unread_count, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	agent_id     = EXCLUDED.agent_id,
	type         = EXCLUDED.type,
	title        = EXCLUDED.title,
	unread_count = EXCLUDED.unread_count,
	updated_at   = EXCLUDED.updated_at`

	_, err := sess.InsertBySql(query,
		row.ID, row.WorkspaceID, row.AgentID, row.Type, row.Title,
		row.UnreadCount, row.CreatedAt, row.UpdatedAt,
	).ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "upsert conversation")
	}

	return nil
}

// MarkAsRead resets the unread_count to zero for a conversation identified by workspace and conversation ID.
// Returns ErrInvalidInput if sess is nil or either ID is empty.
// Returns ErrConversationNotFound if no matching row exists.
func (r *conversationRepo) MarkAsRead(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID string) *merrors.Error {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(conversationID) == "" {
		return merrors.ErrInvalidInput
	}

	result, err := sess.Update("conversations").
		Set("unread_count", 0).
		Set("updated_at", time.Now().UTC()).
		Where("workspace_id = ? AND id = ?", workspaceID, conversationID).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "mark conversation read")
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return merrors.ErrConversationNotFound
	}

	return nil
}
