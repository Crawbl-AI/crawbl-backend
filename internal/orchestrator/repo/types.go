// Package repo hosts shared row, opts, and type aliases used by every
// concrete Postgres repository sub-package. The repository **contracts**
// are no longer declared here — per project convention (consumer-side
// interfaces), each consumer package declares its own narrow interface
// over the concrete repo it holds. This file is now intentionally
// interface-free.
package repo

import (
	"encoding/json"
	"time"

	"github.com/lib/pq"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
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

// UserRow represents a database row for the users table.
// It maps directly to the database schema and provides conversion methods
// to and from the domain model.
type UserRow struct {
	// ID is the unique identifier for the user.
	ID string `db:"id"`
	// Subject is the Firebase authentication UID for the user.
	Subject string `db:"subject"`
	// Email is the user's email address.
	Email string `db:"email"`
	// Nickname is the user's display name/handle.
	Nickname string `db:"nickname"`
	// Name is the user's first name.
	Name string `db:"name"`
	// Surname is the user's last name.
	Surname string `db:"surname"`
	// AvatarURL is an optional URL to the user's profile picture.
	AvatarURL *string `db:"avatar_url"`
	// CountryCode is an optional ISO country code for the user's location.
	CountryCode *string `db:"country_code"`
	// DateOfBirth is an optional date of birth for the user.
	DateOfBirth *time.Time `db:"date_of_birth"`
	// IsBanned indicates whether the user has been banned from the platform.
	IsBanned bool `db:"is_banned"`
	// HasAgreedWithTerms indicates whether the user has accepted the terms of service.
	HasAgreedWithTerms bool `db:"has_agreed_with_terms"`
	// HasAgreedWithPrivacyPolicy indicates whether the user has accepted the privacy policy.
	HasAgreedWithPrivacyPolicy bool `db:"has_agreed_with_privacy_policy"`
	// CreatedAt is the timestamp when the user account was created.
	CreatedAt time.Time `db:"created_at"`
	// UpdatedAt is the timestamp when the user record was last modified.
	UpdatedAt time.Time `db:"updated_at"`
	// DeletedAt is an optional timestamp when the user was soft-deleted.
	DeletedAt *time.Time `db:"deleted_at"`
}

// UserPreferencesRow represents a database row for the user_preferences table.
// It stores user-specific preference settings like theme and language.
type UserPreferencesRow struct {
	// UserID is the foreign key reference to the user.
	UserID string `db:"user_id"`
	// PlatformTheme is an optional preference for the UI theme (e.g., "light", "dark").
	PlatformTheme *string `db:"platform_theme"`
	// PlatformLanguage is an optional preference for the display language.
	PlatformLanguage *string `db:"platform_language"`
	// CurrencyCode is an optional preference for the user's preferred currency.
	CurrencyCode *string `db:"currency_code"`
	// UpdatedAt is the timestamp when preferences were last modified.
	UpdatedAt *time.Time `db:"updated_at"`
}

// UserPushTokenRow represents a database row for the user_push_tokens table.
// It stores push notification tokens for mobile devices.
type UserPushTokenRow struct {
	// UserID is the foreign key reference to the user.
	UserID string `db:"user_id"`
	// PushToken is the device token for push notifications (e.g., FCM token).
	PushToken string `db:"push_token"`
	// UpdatedAt is the timestamp when the push token was last updated.
	UpdatedAt *time.Time `db:"updated_at"`
}

// WorkspaceRow represents a database row for the workspaces table.
// Workspaces are top-level organizational containers owned by users.
type WorkspaceRow struct {
	// ID is the unique identifier for the workspace.
	ID string `db:"id"`
	// UserID is the foreign key reference to the owning user.
	UserID string `db:"user_id"`
	// Name is the display name of the workspace.
	Name string `db:"name"`
	// CreatedAt is the timestamp when the workspace was created.
	CreatedAt time.Time `db:"created_at"`
	// UpdatedAt is the timestamp when the workspace was last modified.
	UpdatedAt time.Time `db:"updated_at"`
}

// AgentRow represents a database row for the agents table.
// Agents are AI assistants within a workspace that can participate in conversations.
type AgentRow struct {
	// ID is the unique identifier for the agent.
	ID string `db:"id"`
	// WorkspaceID is the foreign key reference to the containing workspace.
	WorkspaceID string `db:"workspace_id"`
	// Name is the display name of the agent.
	Name string `db:"name"`
	// Role is the swarm hierarchy role (e.g., "sub-agent", "manager").
	Role string `db:"role"`
	// Slug is the agent runtime routing identifier.
	Slug string `db:"slug"`
	// AvatarURL is the URL to the agent's avatar image.
	AvatarURL string `db:"avatar_url"`
	// SystemPrompt is the LLM system message for this agent's personality.
	SystemPrompt string `db:"system_prompt"`
	// Description is a short human-readable summary of the agent's purpose.
	Description string `db:"description"`
	// SortOrder is the display order of the agent within its workspace.
	SortOrder int `db:"sort_order"`
	// CreatedAt is the timestamp when the agent was created.
	CreatedAt time.Time `db:"created_at"`
	// UpdatedAt is the timestamp when the agent was last modified.
	UpdatedAt time.Time `db:"updated_at"`
}

// ConversationRow represents a database row for the conversations table.
// Conversations are chat threads within a workspace that can involve agents or direct messages.
type ConversationRow struct {
	// ID is the unique identifier for the conversation.
	ID string `db:"id"`
	// WorkspaceID is the foreign key reference to the containing workspace.
	WorkspaceID string `db:"workspace_id"`
	// AgentID is an optional foreign key reference to an agent for agent-specific conversations.
	AgentID *string `db:"agent_id"`
	// Type is the conversation type (e.g., "swarm", "agent", "direct").
	Type string `db:"type"`
	// Title is the display title of the conversation.
	Title string `db:"title"`
	// UnreadCount is the number of unread messages in the conversation.
	UnreadCount int `db:"unread_count"`
	// CreatedAt is the timestamp when the conversation was created.
	CreatedAt time.Time `db:"created_at"`
	// UpdatedAt is the timestamp when the conversation was last modified.
	UpdatedAt time.Time `db:"updated_at"`
}

// MessageRow represents a database row for the messages table.
// Messages are individual chat messages within a conversation.
type MessageRow struct {
	// ID is the unique identifier for the message.
	ID string `db:"id"`
	// ConversationID is the foreign key reference to the containing conversation.
	ConversationID string `db:"conversation_id"`
	// Role is the message role (e.g., "user", "assistant", "system").
	Role string `db:"role"`
	// Content is the JSON-encoded message content.
	Content json.RawMessage `db:"content"`
	// Status is the message status (e.g., "pending", "sent", "failed").
	Status string `db:"status"`
	// LocalID is an optional client-side identifier for offline message tracking.
	LocalID *string `db:"local_id"`
	// AgentID is an optional foreign key reference to an agent for agent responses.
	AgentID *string `db:"agent_id"`
	// Attachments is the JSON-encoded list of message attachments.
	Attachments json.RawMessage `db:"attachments"`
	// CreatedAt is the timestamp when the message was created.
	CreatedAt time.Time `db:"created_at"`
	// UpdatedAt is the timestamp when the message was last modified.
	UpdatedAt time.Time `db:"updated_at"`
}

// CreateUserOpts contains options for creating a new user.
type CreateUserOpts struct {
	// Sess is the database session runner for executing queries.
	Sess SessionRunner
	// User is the domain model containing user data to persist.
	User *orchestrator.User
	// HasAgreedWithLegal indicates whether the user has accepted legal agreements (terms and privacy policy).
	HasAgreedWithLegal bool
}

// ToolRow represents a database row for the tools table.
type ToolRow struct {
	Name        string    `db:"name"`
	DisplayName string    `db:"display_name"`
	Description string    `db:"description"`
	Category    string    `db:"category"`
	IconURL     string    `db:"icon_url"`
	SortOrder   int       `db:"sort_order"`
	CreatedAt   time.Time `db:"created_at"`
}

// AgentSettingsRow represents a database row for the agent_settings table.
type AgentSettingsRow struct {
	AgentID        string         `db:"agent_id"`
	Model          string         `db:"model"`
	ResponseLength string         `db:"response_length"`
	AllowedTools   pq.StringArray `db:"allowed_tools"`
	CreatedAt      time.Time      `db:"created_at"`
	UpdatedAt      time.Time      `db:"updated_at"`
}

// AgentPromptRow represents a database row for the agent_prompts table.
type AgentPromptRow struct {
	ID          string    `db:"id"`
	AgentID     string    `db:"agent_id"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	Content     string    `db:"content"`
	SortOrder   int       `db:"sort_order"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// AgentHistoryRow represents a database row for the agent_history table.
type AgentHistoryRow struct {
	ID             string    `db:"id"`
	AgentID        string    `db:"agent_id"`
	ConversationID *string   `db:"conversation_id"`
	Title          string    `db:"title"`
	Subtitle       string    `db:"subtitle"`
	CreatedAt      time.Time `db:"created_at"`
}
