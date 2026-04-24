// Package messagerepo provides PostgreSQL-based implementation of the MessageRepo interface.
// It handles all database operations related to message entities within conversations.
package messagerepo

import (
	"fmt"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

const (
	whereID             = "id = ?"
	whereConversationID = "conversation_id = ?"

	// defaultListLimit is the default number of messages to return when no limit is specified
	// in the ListByConversationID query.
	defaultListLimit = 50

	// terminalStatusOrdinal is the ordinal assigned to terminal message statuses
	// (failed, incomplete, silent). Terminal statuses can always be written but
	// once set they cannot be overwritten by lower-ordinal statuses.
	terminalStatusOrdinal = 99
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

// messageColumns defines the column names used in SELECT queries for the messages table.
// These columns map directly to the MessageRow struct fields.
var messageColumns = []any{
	"id",
	"conversation_id",
	"role",
	"content",
	"status",
	"local_id",
	"agent_id",
	"attachments",
	"created_at",
	"updated_at",
}

// messageRepo is the PostgreSQL implementation of the MessageRepo interface.
// It handles message data persistence and retrieval operations.
type messageRepo struct{}

// RecordDelegationOpts groups the fields for RecordDelegation. ctx and sess
// remain positional per the project session/opts/repo pattern.
type RecordDelegationOpts struct {
	WorkspaceID      string
	ConversationID   string
	TriggerMsgID     string
	DelegatorAgentID string
	DelegateAgentID  string
	TaskSummary      string
}
