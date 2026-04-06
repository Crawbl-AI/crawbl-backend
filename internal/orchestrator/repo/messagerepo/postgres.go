package messagerepo

import (
	"context"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

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
	for i := range rows {
		message, err := rows[i].ToDomain()
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

// FailStalePending marks all messages with status "pending" created before the
// cutoff time as "failed". Returns the number of affected rows.
// Returns ErrInvalidInput if sess is nil.
func (r *messageRepo) FailStalePending(ctx context.Context, sess orchestratorrepo.SessionRunner, cutoff time.Time) (int, *merrors.Error) {
	if sess == nil {
		return 0, merrors.ErrInvalidInput
	}

	result, err := sess.Update("messages").
		Set("status", "failed").
		Set("updated_at", time.Now().UTC()).
		Where("status = ? AND created_at < ?", "pending", cutoff).
		ExecContext(ctx)
	if err != nil {
		return 0, merrors.WrapStdServerError(err, "fail stale pending messages")
	}

	n, _ := result.RowsAffected()
	return int(n), nil
}

// statusOrdinal returns the monotonic ordering for message statuses.
// Higher ordinals cannot be overwritten by lower ones.
// Terminal statuses (failed, incomplete, silent) get the highest ordinal so
// they can always be written but never overwritten once set.
func statusOrdinal(status orchestrator.MessageStatus) int {
	switch status {
	case orchestrator.MessageStatusPending:
		return 0
	case orchestrator.MessageStatusSent:
		return 1
	case orchestrator.MessageStatusDelivered:
		return 2
	case orchestrator.MessageStatusRead:
		return 3
	case orchestrator.MessageStatusFailed, orchestrator.MessageStatusIncomplete, orchestrator.MessageStatusSilent:
		return terminalStatusOrdinal
	default:
		return -1
	}
}

// UpdateStatus updates just the status and updated_at fields of a message.
// This is a lightweight alternative to Save for status-only transitions.
// A monotonic guard prevents status downgrades using an atomic SQL-side CASE
// expression, eliminating the TOCTOU race of a separate SELECT + UPDATE.
func (r *messageRepo) UpdateStatus(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string, status orchestrator.MessageStatus) *merrors.Error {
	if sess == nil || messageID == "" {
		return merrors.ErrInvalidInput
	}

	newOrd := statusOrdinal(status)

	result, err := sess.Update("messages").
		Set("status", string(status)).
		Set("updated_at", time.Now().UTC()).
		Where("id = ?", messageID).
		Where("CASE status WHEN 'pending' THEN 0 WHEN 'sent' THEN 1 WHEN 'delivered' THEN 2 WHEN 'read' THEN 3 WHEN 'failed' THEN 99 WHEN 'incomplete' THEN 99 WHEN 'silent' THEN 99 ELSE -1 END < ?", newOrd).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "update message status")
	}
	// If no rows were affected the message either does not exist or is already
	// at/past the target ordinal — both are silent no-ops.
	_ = result
	return nil
}

// DeleteByID removes a message by its ID.
func (r *messageRepo) DeleteByID(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string) *merrors.Error {
	if sess == nil || messageID == "" {
		return merrors.ErrInvalidInput
	}
	_, err := sess.DeleteFrom("messages").
		Where("id = ?", messageID).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "delete message by id")
	}
	return nil
}

// GetByID retrieves a single message by its ID.
// Returns ErrMessageNotFound if the message does not exist.
// Returns ErrInvalidInput if sess is nil or messageID is empty.
func (r *messageRepo) GetByID(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string) (*orchestrator.Message, *merrors.Error) {
	if sess == nil || strings.TrimSpace(messageID) == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row orchestratorrepo.MessageRow
	err := sess.Select(orchestratorrepo.Columns(messageColumns...)...).
		From("messages").
		Where("id = ?", messageID).
		LoadOneContext(ctx, &row)
	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return nil, merrors.ErrMessageNotFound
		}
		return nil, merrors.WrapStdServerError(err, "select message by id")
	}

	message, decodeErr := row.ToDomain()
	if decodeErr != nil {
		return nil, merrors.WrapStdServerError(decodeErr, "decode message by id")
	}

	return message, nil
}

// ListRecent retrieves the N most recent messages for a conversation, ordered oldest-first.
func (r *messageRepo) ListRecent(ctx context.Context, sess orchestratorrepo.SessionRunner, conversationID string, limit int) ([]*orchestrator.Message, *merrors.Error) {
	if sess == nil || strings.TrimSpace(conversationID) == "" {
		return nil, merrors.ErrInvalidInput
	}
	if limit <= 0 {
		limit = 20
	}

	var rows []orchestratorrepo.MessageRow
	_, err := sess.Select(orchestratorrepo.Columns(messageColumns...)...).
		From("messages").
		Where("conversation_id = ?", conversationID).
		OrderDesc("created_at").
		Limit(uint64(limit)).
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "list recent messages")
	}

	// Reverse to oldest-first order.
	messages := make([]*orchestrator.Message, len(rows))
	for i := range rows {
		msg, decodeErr := rows[i].ToDomain()
		if decodeErr != nil {
			return nil, merrors.WrapStdServerError(decodeErr, "decode recent message")
		}
		messages[len(rows)-1-i] = msg
	}
	return messages, nil
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

// RecordDelegation inserts an agent_delegations row to track when one agent
// delegates a task to another. This is called asynchronously and is best-effort.
func (r *messageRepo) RecordDelegation(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID, conversationID, triggerMsgID, delegatorAgentID, delegateAgentID, taskSummary string) *merrors.Error {
	if sess == nil {
		return merrors.ErrInvalidInput
	}
	_, err := sess.InsertInto("agent_delegations").
		Pair("workspace_id", workspaceID).
		Pair("conversation_id", conversationID).
		Pair("trigger_message_id", triggerMsgID).
		Pair("delegator_agent_id", delegatorAgentID).
		Pair("delegate_agent_id", delegateAgentID).
		Pair("task_summary", taskSummary).
		Pair("status", "running").
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "insert agent delegation")
	}
	return nil
}

// CompleteDelegation marks a running delegation as completed, recording the
// completion timestamp and elapsed duration in milliseconds.
func (r *messageRepo) CompleteDelegation(ctx context.Context, sess orchestratorrepo.SessionRunner, triggerMsgID, delegateAgentID string) *merrors.Error {
	if sess == nil {
		return merrors.ErrInvalidInput
	}
	_, err := sess.Update("agent_delegations").
		Set("status", "completed").
		Set("completed_at", time.Now().UTC()).
		Set("duration_ms", dbr.Expr("EXTRACT(EPOCH FROM (NOW() - created_at))::INTEGER * 1000")).
		Where("trigger_message_id = ? AND delegate_agent_id = ? AND status = 'running'",
			triggerMsgID, delegateAgentID).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "complete agent delegation")
	}
	return nil
}

// UpdateDelegationSummary backfills the task_summary on delegation
// rows for a given trigger message.
func (r *messageRepo) UpdateDelegationSummary(ctx context.Context, sess orchestratorrepo.SessionRunner, triggerMsgID, summary string) *merrors.Error {
	if sess == nil || summary == "" {
		return nil
	}
	_, err := sess.Update("agent_delegations").
		Set("task_summary", summary).
		Where("trigger_message_id = ? AND task_summary = ''", triggerMsgID).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "update delegation summary")
	}
	return nil
}
