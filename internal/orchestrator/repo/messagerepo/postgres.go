package messagerepo

import (
	"context"
	"strings"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// messageRepo is the PostgreSQL implementation of the MessageRepo interface.
// It handles message data persistence and retrieval operations.
type messageRepo struct{}

// New creates a new MessageRepo instance backed by PostgreSQL.
// The returned repository uses the database session runner pattern for transaction support.
func New() *messageRepo {
	return &messageRepo{}
}

// ListByConversationID retrieves messages from a conversation with cursor-based pagination.
// Messages are returned in descending order by creation time (newest first).
// The cursor-based pagination uses a scroll ID to enable efficient navigation through
// large message histories without offset-based queries.
// Returns an empty page if the scroll ID references a non-existent message.
// Returns ErrInvalidInput if sess is nil, opts is nil, or conversation ID is empty.
//
//nolint:cyclop
func (r *messageRepo) ListByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, opts *orchestratorrepo.ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error) {
	if sess == nil || opts == nil || strings.TrimSpace(opts.ConversationID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}

	query := sess.Select(orchestratorrepo.Columns(messageColumns...)...).
		From("messages").
		Where("conversation_id = ?", opts.ConversationID).
		OrderDesc("created_at").
		OrderDesc("id").
		Limit(uint64(limit + 1))

	if scrollID := strings.TrimSpace(opts.ScrollID); scrollID != "" {
		var cursor struct {
			ID        string    `db:"id"`
			CreatedAt time.Time `db:"created_at"`
		}
		err := sess.Select("id", "created_at").
			From("messages").
			Where("conversation_id = ? AND id = ?", opts.ConversationID, scrollID).
			LoadOneContext(ctx, &cursor)
		if err != nil {
			if database.IsRecordNotFoundError(err) {
				return &orchestrator.MessagePage{
					Data:       []*orchestrator.Message{},
					Pagination: orchestrator.Pagination{},
				}, nil
			}
			return nil, merrors.WrapStdServerError(err, "select message cursor")
		}

		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}

	var rows []orchestratorrepo.MessageRow
	_, err := query.LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list messages by conversation id")
	}

	hasNext := len(rows) > limit
	if hasNext {
		rows = rows[:limit]
	}

	messages := make([]*orchestrator.Message, 0, len(rows))
	for _, row := range rows {
		message, err := row.ToDomain()
		if err != nil {
			return nil, merrors.WrapStdServerError(err, "decode stored message")
		}
		messages = append(messages, message)
	}

	pagination := orchestrator.Pagination{
		HasNext: hasNext,
		HasPrev: strings.TrimSpace(opts.ScrollID) != "",
	}
	if hasNext && len(messages) > 0 {
		pagination.NextScrollID = messages[len(messages)-1].ID
	}
	if strings.TrimSpace(opts.ScrollID) != "" {
		pagination.PrevScrollID = opts.ScrollID
	}

	return &orchestrator.MessagePage{
		Data:       messages,
		Pagination: pagination,
	}, nil
}

// GetLatestByConversationID retrieves the most recent message in a conversation.
// Messages are ordered by creation time descending, so this returns the newest message.
// Returns ErrMessageNotFound if no messages exist in the conversation.
// Returns ErrInvalidInput if sess is nil or conversationID is empty.
func (r *messageRepo) GetLatestByConversationID(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string) (*orchestrator.Message, *merrors.Error) {
	if sess == nil || strings.TrimSpace(conversationID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row orchestratorrepo.MessageRow
	err := sess.Select(orchestratorrepo.Columns(messageColumns...)...).
		From("messages").
		Where("conversation_id = ?", conversationID).
		OrderDesc("created_at").
		OrderDesc("id").
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrMessageNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select latest message by conversation id")
	}

	message, decodeErr := row.ToDomain()
	if decodeErr != nil {
		return nil, merrors.WrapStdServerError(decodeErr, "decode latest message")
	}

	return message, nil
}

// Save persists message data to the database.
// It handles both creating new messages and updating existing ones by checking
// if a message with the same ID exists first.
// The content and attachments are stored as JSON in the database.
// Returns ErrInvalidInput if sess is nil or message is nil.
func (r *messageRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, message *orchestrator.Message) *merrors.Error {
	if sess == nil || message == nil {
		return merrors.ErrInvalidInput
	}

	row, err := orchestratorrepo.NewMessageRow(message)
	if err != nil {
		return merrors.WrapStdServerError(err, "encode message for persistence")
	}

	var existingRow orchestratorrepo.MessageRow
	err = sess.Select(orchestratorrepo.Columns(messageColumns...)...).
		From("messages").
		Where("id = ?", row.ID).
		LoadOneContext(ctx, &existingRow)
	switch {
	case err == nil:
		_, err = sess.Update("messages").
			Set("role", row.Role).
			Set("content", string(row.Content)).
			Set("status", row.Status).
			Set("local_id", row.LocalID).
			Set("agent_id", row.AgentID).
			Set("attachments", string(row.Attachments)).
			Set("updated_at", row.UpdatedAt).
			Where("id = ?", row.ID).
			ExecContext(ctx)
		if err != nil {
			return merrors.WrapStdServerError(err, "update message")
		}
		return nil
	case !database.IsRecordNotFoundError(err):
		return merrors.WrapStdServerError(err, "select message by id for save")
	}

	_, err = sess.InsertInto("messages").
		Pair("id", row.ID).
		Pair("conversation_id", row.ConversationID).
		Pair("role", row.Role).
		Pair("content", string(row.Content)).
		Pair("status", row.Status).
		Pair("local_id", row.LocalID).
		Pair("agent_id", row.AgentID).
		Pair("attachments", string(row.Attachments)).
		Pair("created_at", row.CreatedAt).
		Pair("updated_at", row.UpdatedAt).
		ExecContext(ctx)
	if err != nil {
		if database.IsRecordExistsError(err) {
			_, err = sess.Update("messages").
				Set("role", row.Role).
				Set("content", string(row.Content)).
				Set("status", row.Status).
				Set("local_id", row.LocalID).
				Set("agent_id", row.AgentID).
				Set("attachments", string(row.Attachments)).
				Set("updated_at", row.UpdatedAt).
				Where("id = ?", row.ID).
				ExecContext(ctx)
			if err != nil {
				return merrors.WrapStdServerError(err, "update message after duplicate insert")
			}
			return nil
		}
		return merrors.WrapStdServerError(err, "insert message")
	}

	return nil
}
