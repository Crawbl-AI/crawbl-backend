// Package mcprepo provides persistence operations specific to MCP tool handlers
// that are not covered by existing domain repos.
package mcprepo

import (
	"context"
	"time"

	"github.com/gocraft/dbr/v2"
)

const whereID = "id = ?"

// postgres is the PostgreSQL implementation of the Repo interface.
type postgres struct{}

// agentMessageFinaliseOpts captures the variable bits between
// UpdateAgentMessageCompleted and UpdateAgentMessageFailed.
// FinalCol is either "response_text" or "error_message".
type agentMessageFinaliseOpts struct {
	ID         string
	Status     string
	FinalCol   string
	FinalVal   string
	OpLabel    string
	DurationMs int64
}

// Repo defines the persistence operations specific to MCP tool handlers
// that are not covered by existing domain repos.
type Repo interface {
	// User
	GetUserByID(ctx context.Context, sess *dbr.Session, userID string) (*UserRow, error)
	GetUserPreferences(ctx context.Context, sess *dbr.Session, userID string) (*UserPreferencesRow, error)
	GetPushToken(ctx context.Context, sess *dbr.Session, userID string) (string, error)

	// Message search
	SearchMessages(ctx context.Context, sess *dbr.Session, conversationID, query string, limit int) ([]MessageSearchRow, error)

	// Agent messages (inter-agent delegation)
	CreateAgentMessage(ctx context.Context, sess *dbr.Session, row *AgentMessageRow) error
	UpdateAgentMessageCompleted(ctx context.Context, sess *dbr.Session, id, responseText string, durationMs int64) error
	UpdateAgentMessageFailed(ctx context.Context, sess *dbr.Session, id, errMsg string, durationMs int64) error
	GetMaxAgentMessageDepth(ctx context.Context, sess *dbr.Session, workspaceID, conversationID string) (int, error)

	// Artifacts
	UpdateArtifactStatus(ctx context.Context, sess *dbr.Session, artifactID, status string) error
}

// UserRow holds the fields returned by the user profile query.
type UserRow struct {
	ID          string    `db:"id"`
	Email       string    `db:"email"`
	Nickname    string    `db:"nickname"`
	Name        string    `db:"name"`
	Surname     string    `db:"surname"`
	CountryCode *string   `db:"country_code"`
	CreatedAt   time.Time `db:"created_at"`
}

// UserPreferencesRow holds the fields returned by the user preferences query.
type UserPreferencesRow struct {
	Theme    *string `db:"platform_theme"`
	Language *string `db:"platform_language"`
	Currency *string `db:"currency_code"`
}

// MessageSearchRow holds the fields returned by a message search query.
type MessageSearchRow struct {
	ID        string    `db:"id"`
	Role      string    `db:"role"`
	Content   string    `db:"content"`
	CreatedAt time.Time `db:"created_at"`
}

// AgentMessageRow holds the fields for an agent_messages insert.
type AgentMessageRow struct {
	ID             string
	WorkspaceID    string
	ConversationID string
	FromAgentID    string
	FromAgentSlug  string
	ToAgentID      string
	ToAgentSlug    string
	RequestText    string
	Status         string
	Depth          int
}
