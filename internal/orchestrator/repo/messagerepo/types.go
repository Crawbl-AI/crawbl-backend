// Package messagerepo provides PostgreSQL-based implementation of the MessageRepo interface.
// It handles all database operations related to message entities within conversations.
package messagerepo

// messageRepo is the PostgreSQL implementation of the MessageRepo interface.
// It handles message data persistence and retrieval operations.
type messageRepo struct{}

// defaultListLimit is the default number of messages to return when no limit is specified
// in the ListByConversationID query.
const defaultListLimit = 50

// messageColumns defines the column names used in SELECT queries for the messages table.
// These columns map directly to the MessageRow struct fields.
var messageColumns = []string{
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
