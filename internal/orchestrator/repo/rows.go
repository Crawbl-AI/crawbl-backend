// Package repo provides repository interfaces and database row types for the orchestrator layer.
// It defines the data access contracts and persistence row mappings for users, workspaces,
// agents, conversations, and messages.
package repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lib/pq"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// SessionRunner is an alias for database.SessionRunner, providing transaction and query execution capabilities.
// It allows repository methods to work with either a direct database connection or a transaction.
type SessionRunner = database.SessionRunner

// Columns converts a variadic list of string column names into a slice of interface{} values.
// This is a utility function used to build SELECT column lists for database queries.
func Columns(columns ...string) []any {
	converted := make([]any, 0, len(columns))
	for _, column := range columns {
		converted = append(converted, column)
	}

	return converted
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

// NewUserRow creates a UserRow from a domain User model.
// Returns nil if the input user is nil.
func NewUserRow(user *orchestrator.User) *UserRow {
	if user == nil {
		return nil
	}

	return &UserRow{
		ID:                         user.ID,
		Subject:                    user.Subject,
		Email:                      user.Email,
		Nickname:                   user.Nickname,
		Name:                       user.Name,
		Surname:                    user.Surname,
		AvatarURL:                  user.AvatarURL,
		CountryCode:                user.CountryCode,
		DateOfBirth:                user.DateOfBirth,
		IsBanned:                   user.IsBanned,
		HasAgreedWithTerms:         user.HasAgreedWithTerms,
		HasAgreedWithPrivacyPolicy: user.HasAgreedWithPrivacyPolicy,
		CreatedAt:                  user.CreatedAt,
		UpdatedAt:                  user.UpdatedAt,
		DeletedAt:                  user.DeletedAt,
	}
}

// ToDomain converts a UserRow to its domain model representation.
func (r *UserRow) ToDomain() *orchestrator.User {
	return &orchestrator.User{
		ID:                         r.ID,
		Subject:                    r.Subject,
		Email:                      r.Email,
		Nickname:                   r.Nickname,
		Name:                       r.Name,
		Surname:                    r.Surname,
		AvatarURL:                  r.AvatarURL,
		CountryCode:                r.CountryCode,
		DateOfBirth:                r.DateOfBirth,
		IsBanned:                   r.IsBanned,
		HasAgreedWithTerms:         r.HasAgreedWithTerms,
		HasAgreedWithPrivacyPolicy: r.HasAgreedWithPrivacyPolicy,
		CreatedAt:                  r.CreatedAt,
		UpdatedAt:                  r.UpdatedAt,
		DeletedAt:                  r.DeletedAt,
	}
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

// NewUserPreferencesRow creates a UserPreferencesRow from a domain User model.
// Returns nil if the input user is nil.
func NewUserPreferencesRow(user *orchestrator.User) *UserPreferencesRow {
	if user == nil {
		return nil
	}

	return &UserPreferencesRow{
		UserID:           user.ID,
		PlatformTheme:    user.Preferences.PlatformTheme,
		PlatformLanguage: user.Preferences.PlatformLanguage,
		CurrencyCode:     user.Preferences.CurrencyCode,
		UpdatedAt:        &user.UpdatedAt,
	}
}

// ApplyToUser populates the preferences fields on a domain User model from the row data.
// This method modifies the user in place.
func (r *UserPreferencesRow) ApplyToUser(user *orchestrator.User) {
	if user == nil || r == nil {
		return
	}

	user.Preferences.PlatformTheme = r.PlatformTheme
	user.Preferences.PlatformLanguage = r.PlatformLanguage
	user.Preferences.CurrencyCode = r.CurrencyCode
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

// NewUserPushTokenRow creates a UserPushTokenRow with the given parameters.
func NewUserPushTokenRow(userID, pushToken string, updatedAt time.Time) *UserPushTokenRow {
	return &UserPushTokenRow{
		UserID:    userID,
		PushToken: pushToken,
		UpdatedAt: &updatedAt,
	}
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

// NewWorkspaceRow creates a WorkspaceRow from a domain Workspace model.
// Returns nil if the input workspace is nil.
func NewWorkspaceRow(workspace *orchestrator.Workspace) *WorkspaceRow {
	if workspace == nil {
		return nil
	}

	return &WorkspaceRow{
		ID:        workspace.ID,
		UserID:    workspace.UserID,
		Name:      workspace.Name,
		CreatedAt: workspace.CreatedAt,
		UpdatedAt: workspace.UpdatedAt,
	}
}

// ToDomain converts a WorkspaceRow to its domain model representation.
func (r *WorkspaceRow) ToDomain() *orchestrator.Workspace {
	return &orchestrator.Workspace{
		ID:        r.ID,
		UserID:    r.UserID,
		Name:      r.Name,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
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

// NewAgentRow creates an AgentRow from a domain Agent model.
// The sortOrder parameter specifies the display order within the workspace.
// Returns nil if the input agent is nil.
func NewAgentRow(agent *orchestrator.Agent, sortOrder int) *AgentRow {
	if agent == nil {
		return nil
	}

	return &AgentRow{
		ID:           agent.ID,
		WorkspaceID:  agent.WorkspaceID,
		Name:         agent.Name,
		Role:         agent.Role,
		Slug:         agent.Slug,
		AvatarURL:    agent.AvatarURL,
		SystemPrompt: agent.SystemPrompt,
		Description:  agent.Description,
		SortOrder:    sortOrder,
		CreatedAt:    agent.CreatedAt,
		UpdatedAt:    agent.UpdatedAt,
	}
}

// ToDomain converts an AgentRow to its domain model representation.
func (r *AgentRow) ToDomain() *orchestrator.Agent {
	return &orchestrator.Agent{
		ID:           r.ID,
		WorkspaceID:  r.WorkspaceID,
		Name:         r.Name,
		Role:         r.Role,
		Slug:         r.Slug,
		AvatarURL:    r.AvatarURL,
		SystemPrompt: r.SystemPrompt,
		Description:  r.Description,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
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

// NewConversationRow creates a ConversationRow from a domain Conversation model.
// Returns nil if the input conversation is nil.
func NewConversationRow(conversation *orchestrator.Conversation) *ConversationRow {
	if conversation == nil {
		return nil
	}

	return &ConversationRow{
		ID:          conversation.ID,
		WorkspaceID: conversation.WorkspaceID,
		AgentID:     conversation.AgentID,
		Type:        string(conversation.Type),
		Title:       conversation.Title,
		UnreadCount: conversation.UnreadCount,
		CreatedAt:   conversation.CreatedAt,
		UpdatedAt:   conversation.UpdatedAt,
	}
}

// ToDomain converts a ConversationRow to its domain model representation.
func (r *ConversationRow) ToDomain() *orchestrator.Conversation {
	return &orchestrator.Conversation{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		AgentID:     r.AgentID,
		Type:        orchestrator.ConversationType(r.Type),
		Title:       r.Title,
		UnreadCount: r.UnreadCount,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
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

// NewMessageRow creates a MessageRow from a domain Message model.
// Returns nil if the input message is nil.
// Returns an error if JSON marshaling of content or attachments fails.
func NewMessageRow(message *orchestrator.Message) (*MessageRow, error) {
	if message == nil {
		return nil, nil
	}

	content, err := json.Marshal(message.Content)
	if err != nil {
		return nil, err
	}
	attachments, err := json.Marshal(message.Attachments)
	if err != nil {
		return nil, err
	}

	return &MessageRow{
		ID:             message.ID,
		ConversationID: message.ConversationID,
		Role:           string(message.Role),
		Content:        content,
		Status:         string(message.Status),
		LocalID:        message.LocalID,
		AgentID:        message.AgentID,
		Attachments:    attachments,
		CreatedAt:      message.CreatedAt,
		UpdatedAt:      message.UpdatedAt,
	}, nil
}

// ToDomain converts a MessageRow to its domain model representation.
// Returns an error if JSON unmarshaling of content or attachments fails.
func (r *MessageRow) ToDomain() (*orchestrator.Message, error) {
	var content orchestrator.MessageContent
	if len(r.Content) > 0 {
		if err := json.Unmarshal(r.Content, &content); err != nil {
			return nil, err
		}
	}

	attachments := make([]orchestrator.Attachment, 0)
	if len(r.Attachments) > 0 {
		if err := json.Unmarshal(r.Attachments, &attachments); err != nil {
			return nil, err
		}
	}

	return &orchestrator.Message{
		ID:             r.ID,
		ConversationID: r.ConversationID,
		Role:           orchestrator.MessageRole(r.Role),
		Content:        content,
		Status:         orchestrator.MessageStatus(r.Status),
		LocalID:        r.LocalID,
		AgentID:        r.AgentID,
		Attachments:    attachments,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}, nil
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

// UserRepo defines the repository interface for user data access operations.
// Implementations handle persisting and retrieving user data from the database.
type UserRepo interface {
	// GetBySubject retrieves a user by their Firebase authentication subject (UID).
	GetBySubject(ctx context.Context, sess SessionRunner, subject string) (*orchestrator.User, *merrors.Error)
	// GetUser retrieves a user by email (preferred) or subject.
	// If found by email but subject doesn't match, returns ErrUserWrongFirebaseUID.
	GetUser(ctx context.Context, sess SessionRunner, subject, email string) (*orchestrator.User, *merrors.Error)
	// CreateUser creates a new user with the specified legal agreement status.
	// Returns an error if the user already exists.
	CreateUser(ctx context.Context, opts *CreateUserOpts) *merrors.Error
	// Save persists user data, creating a new record or updating an existing one.
	Save(ctx context.Context, sess SessionRunner, user *orchestrator.User) *merrors.Error
	// SavePushToken stores or updates a push notification token for a user.
	SavePushToken(ctx context.Context, sess SessionRunner, userID, pushToken string) *merrors.Error
	// ClearPushTokens removes all push notification tokens for a user.
	ClearPushTokens(ctx context.Context, sess SessionRunner, userID string) *merrors.Error
	// IsUserDeleted checks if a user exists in the deleted users table.
	IsUserDeleted(ctx context.Context, sess SessionRunner, subject, email string) (bool, *merrors.Error)
	// CheckNicknameExists checks if a nickname is already taken by another user.
	CheckNicknameExists(ctx context.Context, sess SessionRunner, nickname string) (bool, *merrors.Error)
}

// WorkspaceRepo defines the repository interface for workspace data access operations.
// Workspaces are top-level organizational containers for users.
type WorkspaceRepo interface {
	// ListByUserID retrieves all workspaces owned by a user.
	ListByUserID(ctx context.Context, sess SessionRunner, userID string) ([]*orchestrator.Workspace, *merrors.Error)
	// GetByID retrieves a specific workspace by ID, verifying ownership.
	GetByID(ctx context.Context, sess SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
	// Save persists workspace data, creating a new record or updating an existing one.
	Save(ctx context.Context, sess SessionRunner, workspace *orchestrator.Workspace) *merrors.Error
}

// AgentRepo defines the repository interface for agent data access operations.
// Agents are AI assistants within workspaces.
type AgentRepo interface {
	// ListByWorkspaceID retrieves all agents within a workspace, ordered by sort order.
	ListByWorkspaceID(ctx context.Context, sess SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error)
	// GetByID retrieves a specific agent by ID, verifying workspace membership.
	GetByID(ctx context.Context, sess SessionRunner, workspaceID, agentID string) (*orchestrator.Agent, *merrors.Error)
	// GetByIDGlobal retrieves a specific agent by ID without workspace filtering.
	GetByIDGlobal(ctx context.Context, sess SessionRunner, agentID string) (*orchestrator.Agent, *merrors.Error)
	// CountMessagesByAgentID counts the total number of messages attributed to an agent.
	CountMessagesByAgentID(ctx context.Context, sess SessionRunner, agentID string) (int, *merrors.Error)
	// Save persists agent data with a specified sort order.
	Save(ctx context.Context, sess SessionRunner, agent *orchestrator.Agent, sortOrder int) *merrors.Error
}

// ConversationRepo defines the repository interface for conversation data access operations.
// Conversations are chat threads within workspaces.
type ConversationRepo interface {
	// ListByWorkspaceID retrieves all conversations within a workspace.
	ListByWorkspaceID(ctx context.Context, sess SessionRunner, workspaceID string) ([]*orchestrator.Conversation, *merrors.Error)
	// GetByID retrieves a specific conversation by ID, verifying workspace membership.
	GetByID(ctx context.Context, sess SessionRunner, workspaceID, conversationID string) (*orchestrator.Conversation, *merrors.Error)
	// FindDefaultSwarm retrieves the default swarm conversation for a workspace.
	FindDefaultSwarm(ctx context.Context, sess SessionRunner, workspaceID string) (*orchestrator.Conversation, *merrors.Error)
	// Save persists conversation data, creating a new record or updating an existing one.
	Save(ctx context.Context, sess SessionRunner, conversation *orchestrator.Conversation) *merrors.Error
	// Create inserts a new conversation record.
	Create(ctx context.Context, sess SessionRunner, conversation *orchestrator.Conversation) *merrors.Error
	// Delete removes a conversation by ID within a workspace.
	Delete(ctx context.Context, sess SessionRunner, workspaceID, conversationID string) *merrors.Error
	// MarkAsRead resets the unread count for a conversation to zero.
	MarkAsRead(ctx context.Context, sess SessionRunner, workspaceID, conversationID string) *merrors.Error
}

// ListMessagesOpts contains options for listing messages with pagination.
type ListMessagesOpts struct {
	// ConversationID is the ID of the conversation to list messages from.
	ConversationID string
	// ScrollID is an optional cursor for cursor-based pagination.
	ScrollID string
	// Limit is the maximum number of messages to return.
	Limit int
}

// MessageRepo defines the repository interface for message data access operations.
// Messages are individual chat messages within conversations.
type MessageRepo interface {
	// ListByConversationID retrieves messages from a conversation with cursor-based pagination.
	ListByConversationID(ctx context.Context, sess SessionRunner, opts *ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error)
	// GetLatestByConversationID retrieves the most recent message in a conversation.
	GetLatestByConversationID(ctx context.Context, sess SessionRunner, conversationID string) (*orchestrator.Message, *merrors.Error)
	// GetByID retrieves a single message by its ID.
	GetByID(ctx context.Context, sess SessionRunner, messageID string) (*orchestrator.Message, *merrors.Error)
	// Save persists message data, creating a new record or updating an existing one.
	Save(ctx context.Context, sess SessionRunner, message *orchestrator.Message) *merrors.Error
	// FailStalePending marks all messages with status "pending" created before
	// the cutoff time as "failed". Returns the number of affected rows.
	// Used by the background cleanup to handle orphaned streaming placeholders.
	FailStalePending(ctx context.Context, sess SessionRunner, cutoff time.Time) (int, *merrors.Error)
	// UpdateStatus updates just the status and updated_at of a message by ID.
	UpdateStatus(ctx context.Context, sess SessionRunner, messageID string, status orchestrator.MessageStatus) *merrors.Error
	// DeleteByID removes a message by its ID.
	DeleteByID(ctx context.Context, sess SessionRunner, messageID string) *merrors.Error
	// ListRecent retrieves the N most recent messages for a conversation, ordered oldest-first.
	// Used for building conversation context to inject into agent calls.
	ListRecent(ctx context.Context, sess SessionRunner, conversationID string, limit int) ([]*orchestrator.Message, *merrors.Error)
	// RecordDelegation inserts an agent_delegations row to track when one agent
	// delegates a task to another agent within a conversation.
	RecordDelegation(ctx context.Context, sess SessionRunner, workspaceID, conversationID, triggerMsgID, delegatorAgentID, delegateAgentID, taskSummary string) *merrors.Error
	// CompleteDelegation marks a running delegation as completed, recording the
	// agent response as a delivered message in the conversation.
	CompleteDelegation(ctx context.Context, sess SessionRunner, triggerMsgID, delegateAgentID string) *merrors.Error
}

// ToolsRepo defines the repository interface for tool catalog operations.
type ToolsRepo interface {
	List(ctx context.Context, sess SessionRunner, limit, offset int, category string) ([]orchestrator.AgentTool, *merrors.Error)
	Count(ctx context.Context, sess SessionRunner, category string) (int, *merrors.Error)
	GetByNames(ctx context.Context, sess SessionRunner, names []string) ([]orchestrator.AgentTool, *merrors.Error)
	Seed(ctx context.Context, sess SessionRunner, tools []ToolRow) *merrors.Error
}

// AgentSettingsRepo defines the repository interface for agent settings operations.
type AgentSettingsRepo interface {
	GetByAgentID(ctx context.Context, sess SessionRunner, agentID string) (*AgentSettingsRow, *merrors.Error)
	Save(ctx context.Context, sess SessionRunner, row *AgentSettingsRow) *merrors.Error
}

// AgentPromptsRepo defines the repository interface for agent prompt operations.
type AgentPromptsRepo interface {
	ListByAgentID(ctx context.Context, sess SessionRunner, agentID string) ([]AgentPromptRow, *merrors.Error)
	BulkSave(ctx context.Context, sess SessionRunner, rows []AgentPromptRow) *merrors.Error
}

// AgentHistoryRepo defines the repository interface for agent history operations.
type AgentHistoryRepo interface {
	ListByAgentID(ctx context.Context, sess SessionRunner, agentID string, limit, offset int) ([]AgentHistoryRow, *merrors.Error)
	CountByAgentID(ctx context.Context, sess SessionRunner, agentID string) (int, *merrors.Error)
	Create(ctx context.Context, sess SessionRunner, row *AgentHistoryRow) *merrors.Error
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

// ModelRow represents a database row for the models table.
type ModelRow struct {
	ID          string    `db:"id"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	SortOrder   int       `db:"sort_order"`
	CreatedAt   time.Time `db:"created_at"`
}

// ToolCategoryRow represents a database row for the tool_categories table.
type ToolCategoryRow struct {
	ID        string    `db:"id"`
	Name      string    `db:"name"`
	ImageURL  string    `db:"image_url"`
	SortOrder int       `db:"sort_order"`
	CreatedAt time.Time `db:"created_at"`
}

// IntegrationCategoryRow represents a database row for the integration_categories table.
type IntegrationCategoryRow struct {
	ID        string    `db:"id"`
	Name      string    `db:"name"`
	ImageURL  string    `db:"image_url"`
	SortOrder int       `db:"sort_order"`
	CreatedAt time.Time `db:"created_at"`
}

// IntegrationProviderRow represents a database row for the integration_providers table.
type IntegrationProviderRow struct {
	Provider    string    `db:"provider"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	IconURL     string    `db:"icon_url"`
	CategoryID  string    `db:"category_id"`
	IsEnabled   bool      `db:"is_enabled"`
	SortOrder   int       `db:"sort_order"`
	CreatedAt   time.Time `db:"created_at"`
}
