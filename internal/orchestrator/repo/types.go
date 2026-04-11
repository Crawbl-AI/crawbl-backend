package repo

import (
	"context"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// SessionRunner is an alias for database.SessionRunner, providing transaction and query execution capabilities.
// It allows repository methods to work with either a direct database connection or a transaction.
type SessionRunner = database.SessionRunner

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
	// ListOwnedByUser returns the subset of workspaceIDs owned by userID as a
	// set for O(1) membership tests. Issues a single SELECT ... WHERE id IN (...)
	// query regardless of how many IDs are requested.
	ListOwnedByUser(ctx context.Context, sess SessionRunner, userID string, workspaceIDs []string) (map[string]struct{}, *merrors.Error)
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
	// GetLatestByConversationIDs returns the most-recent message per conversation
	// using a single query. Missing conversations are omitted from the result map.
	GetLatestByConversationIDs(ctx context.Context, sess SessionRunner, conversationIDs []string) (map[string]*orchestrator.Message, *merrors.Error)
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
	// UpdateDelegationSummary backfills the task_summary on delegation rows
	// for a given trigger message. Called after the Manager's reasoning text
	// is fully accumulated.
	UpdateDelegationSummary(ctx context.Context, sess SessionRunner, triggerMsgID, summary string) *merrors.Error
	// UpdateToolState updates a tool_status message's state (running → completed/failed).
	UpdateToolState(ctx context.Context, sess SessionRunner, messageID string, state string) *merrors.Error
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

// IntegrationConnRepo defines the repository interface for integration connection operations.
type IntegrationConnRepo interface {
	// ListActiveProviders returns provider names with active connections for a user.
	ListActiveProviders(ctx context.Context, sess SessionRunner, userID, activeStatus string) ([]string, *merrors.Error)
}
