package messagerepo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// messageStatusOrderingCASE builds the SQL CASE expression for monotonic status ordering.
// Higher ordinals prevent downgrades to lower-status states.
var messageStatusOrderingCASE = fmt.Sprintf(
	"CASE status WHEN '%s' THEN 0 WHEN '%s' THEN 1 WHEN '%s' THEN 2 WHEN '%s' THEN 3 WHEN '%s' THEN 99 WHEN '%s' THEN 99 WHEN '%s' THEN 99 ELSE -1 END",
	orchestrator.MessageStatusPending,
	orchestrator.MessageStatusSent,
	orchestrator.MessageStatusDelivered,
	orchestrator.MessageStatusRead,
	orchestrator.MessageStatusFailed,
	orchestrator.MessageStatusIncomplete,
	orchestrator.MessageStatusSilent,
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

// GetLatestByConversationIDs returns the most-recent message per conversation
// using a single query rather than one round-trip per conversation. It fetches
// all messages for the given IDs ordered newest-first, then picks the first
// (latest) message for each conversation ID in Go — O(n) in memory, one DB
// round-trip regardless of the number of conversations.
//
// DISTINCT ON (conversation_id) would express the same intent in pure SQL but
// dbr does not expose that Postgres-specific syntax natively; the in-memory
// grouping avoids a SelectBySql call while keeping the code readable.
//
// Missing conversations are simply omitted from the result map.
func (r *messageRepo) GetLatestByConversationIDs(
	ctx context.Context,
	sess orchestratorrepo.SessionRunner,
	conversationIDs []string,
) (map[string]*orchestrator.Message, *merrors.Error) {
	if sess == nil {
		return nil, merrors.ErrInvalidInput
	}
	if len(conversationIDs) == 0 {
		return map[string]*orchestrator.Message{}, nil
	}

	// Convert []string to []any for dbr's IN clause.
	args := make([]any, len(conversationIDs))
	for i, id := range conversationIDs {
		args[i] = id
	}

	var rows []orchestratorrepo.MessageRow
	_, err := sess.Select(orchestratorrepo.Columns(messageColumns...)...).
		From("messages").
		Where("conversation_id IN ?", args).
		OrderDesc("created_at").
		OrderDesc("id").
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, merrors.WrapStdServerError(err, "get latest messages by conversation ids")
	}

	// Pick the first (latest) row per conversation_id — rows are already
	// ordered newest-first so the first occurrence wins.
	result := make(map[string]*orchestrator.Message, len(conversationIDs))
	for i := range rows {
		msg, decodeErr := rows[i].ToDomain()
		if decodeErr != nil {
			return nil, merrors.WrapStdServerError(decodeErr, "decode latest message by conversation ids")
		}
		if _, seen := result[msg.ConversationID]; !seen {
			result[msg.ConversationID] = msg
		}
	}
	return result, nil
}

// FailStalePending marks all messages with status "pending" created before the
// cutoff time as "failed". Returns the number of affected rows.
// Returns ErrInvalidInput if sess is nil.
func (r *messageRepo) FailStalePending(ctx context.Context, sess orchestratorrepo.SessionRunner, cutoff time.Time) (int, *merrors.Error) {
	if sess == nil {
		return 0, merrors.ErrInvalidInput
	}

	result, err := sess.Update("messages").
		Set("status", string(orchestrator.MessageStatusFailed)).
		Set("updated_at", time.Now().UTC()).
		Where("status = ? AND created_at < ?", string(orchestrator.MessageStatusPending), cutoff).
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
		Where(messageStatusOrderingCASE+" < ?", newOrd).
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
// The content and attachments are stored as JSON in the database.
// Returns ErrInvalidInput if sess is nil or message is nil.
// Raw SQL: dbr has no ON CONFLICT builder.
func (r *messageRepo) Save(ctx context.Context, sess orchestratorrepo.SessionRunner, message *orchestrator.Message) *merrors.Error {
	if sess == nil || message == nil {
		return merrors.ErrInvalidInput
	}

	row, err := orchestratorrepo.NewMessageRow(message)
	if err != nil {
		return merrors.WrapStdServerError(err, "encode message for persistence")
	}

	const query = `
INSERT INTO messages (
	id, conversation_id, role, content, status, local_id,
	agent_id, attachments, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	role        = EXCLUDED.role,
	content     = EXCLUDED.content,
	status      = EXCLUDED.status,
	local_id    = EXCLUDED.local_id,
	agent_id    = EXCLUDED.agent_id,
	attachments = EXCLUDED.attachments,
	updated_at  = EXCLUDED.updated_at`

	_, dbErr := sess.InsertBySql(query,
		row.ID, row.ConversationID, row.Role, string(row.Content), row.Status, row.LocalID,
		row.AgentID, string(row.Attachments), row.CreatedAt, row.UpdatedAt,
	).ExecContext(ctx)
	if dbErr != nil {
		return merrors.WrapStdServerError(dbErr, "upsert message")
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
		Pair("status", string(orchestrator.MessageStatusRead)).
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
		Set("status", string(orchestrator.MessageStatusRead)).
		Set("completed_at", time.Now().UTC()).
		Set("duration_ms", dbr.Expr("EXTRACT(EPOCH FROM (NOW() - created_at))::INTEGER * 1000")).
		Where("trigger_message_id = ? AND delegate_agent_id = ? AND status = ?",
			triggerMsgID, delegateAgentID, string(orchestrator.MessageStatusRead)).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "complete agent delegation")
	}
	return nil
}

// UpdateToolState updates the state field inside a tool_status message's JSONB content.
func (r *messageRepo) UpdateToolState(ctx context.Context, sess orchestratorrepo.SessionRunner, messageID string, state string) *merrors.Error {
	if sess == nil || messageID == "" {
		return nil
	}
	_, err := sess.Update("messages").
		Set("content", dbr.Expr("jsonb_set(content, '{state}', to_jsonb(?::text))", state)).
		Set("updated_at", time.Now().UTC()).
		Where("id = ?", messageID).
		ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, "update tool state")
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
