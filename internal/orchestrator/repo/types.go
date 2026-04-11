// Package repo hosts shared row, opts, and type aliases used by every
// concrete Postgres repository sub-package. The repository **contracts**
// are no longer declared here — per project convention (consumer-side
// interfaces), each consumer package declares its own narrow interface
// over the concrete repo it holds. This file is now intentionally
// interface-free.
package repo

import (
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// SessionRunner is an alias for database.SessionRunner, providing
// transaction and query execution capabilities. Repository methods use
// it as their first argument so callers can pass either a direct
// *dbr.Session or a transaction-wrapped runner.
type SessionRunner = database.SessionRunner

// ListMessagesOpts contains options for listing messages with
// cursor-based pagination. Declared here because both the messagerepo
// struct and the chatservice consumer interface (chatservice/ports.go)
// reference it.
type ListMessagesOpts struct {
	// ConversationID is the ID of the conversation to list messages from.
	ConversationID string
	// ScrollID is an optional cursor for cursor-based pagination.
	ScrollID string
	// Limit is the maximum number of messages to return.
	Limit int
}
