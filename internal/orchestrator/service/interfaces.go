// Package service — this file declares all service interface definitions and
// the WorkspaceBootstrapper interface. Options structs and domain types used
// by these interfaces live in types.go.
package service

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// WorkspaceBootstrapper defines the interface for workspace initialization.
// Implementations are responsible for creating the default workspace and
// associated resources for a new user.
type WorkspaceBootstrapper interface {
	// EnsureDefaultWorkspace creates the default workspace for a user if it does
	// not already exist. This operation is idempotent - calling it multiple times
	// with the same user ID will not create duplicate workspaces.
	//
	// Returns a merrors.Error if the operation fails, or nil on success.
	EnsureDefaultWorkspace(ctx context.Context, opts *EnsureDefaultWorkspaceOpts) *merrors.Error
}

// AuthService defines the interface for user authentication and account management.
// Implementations handle user registration, authentication, profile updates,
// and legal document acceptance.
type AuthService interface {
	// SignUp registers a new user in the system. This creates the user account
	// and may trigger workspace provisioning. The operation is idempotent for
	// existing users based on their subject identifier.
	//
	// Returns the created User on success, or a merrors.Error on failure.
	SignUp(ctx context.Context, opts *SignUpOpts) (*orchestrator.User, *merrors.Error)

	// SignIn authenticates an existing user and returns their user profile.
	// This validates the user's credentials and ensures the account is active.
	//
	// Returns the authenticated User on success, or a merrors.Error on failure.
	SignIn(ctx context.Context, opts *SignInOpts) (*orchestrator.User, *merrors.Error)

	// Delete removes a user account from the system. This may perform either
	// a soft delete (marking the account as deleted) or a hard delete depending
	// on the implementation, while maintaining an audit trail.
	//
	// Returns a merrors.Error if the operation fails, or nil on success.
	Delete(ctx context.Context, opts *DeleteOpts) *merrors.Error

	// GetBySubject retrieves a user by their external identity subject identifier.
	// This is typically used to look up users by their authentication provider ID.
	//
	// Returns the User on success, or a merrors.Error if not found or on failure.
	GetBySubject(ctx context.Context, opts *GetUserBySubjectOpts) (*orchestrator.User, *merrors.Error)

	// UpdateProfile modifies a user's profile information. Only non-nil fields
	// in the options struct will be updated; nil fields are ignored.
	//
	// Returns the updated User on success, or a merrors.Error on failure.
	UpdateProfile(ctx context.Context, opts *UpdateProfileOpts) (*orchestrator.User, *merrors.Error)

	// GetLegalDocuments retrieves the current legal documents (terms of service,
	// privacy policy) that users must accept. This is typically used to display
	// the documents for user review before acceptance.
	//
	// Returns the LegalDocuments on success, or a merrors.Error on failure.
	GetLegalDocuments(ctx context.Context) (*orchestrator.LegalDocuments, *merrors.Error)

	// AcceptLegal records a user's acceptance of specific versions of legal
	// documents. This creates an audit trail of when and which documents were accepted.
	//
	// Returns the updated User on success, or a merrors.Error on failure.
	AcceptLegal(ctx context.Context, opts *AcceptLegalOpts) (*orchestrator.User, *merrors.Error)

	// SavePushToken registers a push notification token for a user's device.
	// This enables the system to send push notifications to the user.
	//
	// Returns a merrors.Error if the operation fails, or nil on success.
	SavePushToken(ctx context.Context, opts *SavePushTokenOpts) *merrors.Error
}

// WorkspaceService defines the interface for workspace management operations.
// Implementations handle workspace creation, listing, and retrieval.
type WorkspaceService interface {
	// EnsureDefaultWorkspace creates the default workspace for a user if it does
	// not already exist. This is typically called during user provisioning and
	// may trigger Kubernetes agent runtime resource creation.
	//
	// Returns a merrors.Error if the operation fails, or nil on success.
	EnsureDefaultWorkspace(ctx context.Context, opts *EnsureDefaultWorkspaceOpts) *merrors.Error

	// ListByUserID retrieves all workspaces associated with a specific user.
	// The returned workspaces may include runtime status information from the
	// associated agent runtime resources.
	//
	// Returns a slice of Workspace pointers on success, or a merrors.Error on failure.
	ListByUserID(ctx context.Context, opts *ListWorkspacesOpts) ([]*orchestrator.Workspace, *merrors.Error)

	// GetByID retrieves a specific workspace by its ID, ensuring the user has
	// access to that workspace.
	//
	// Returns the Workspace on success, or a merrors.Error if not found or on failure.
	GetByID(ctx context.Context, opts *GetWorkspaceOpts) (*orchestrator.Workspace, *merrors.Error)
}

// ChatService defines the interface for chat and messaging operations.
// Implementations handle agents, conversations, and messages within workspaces.
type ChatService interface {
	// ListAgents retrieves all agents available in a specific workspace.
	// Agents represent the AI swarm members that users can interact with.
	//
	// Returns a slice of Agent pointers on success, or a merrors.Error on failure.
	ListAgents(ctx context.Context, opts *ListAgentsOpts) ([]*orchestrator.Agent, *merrors.Error)

	// ListConversations retrieves all conversations in a specific workspace.
	// Conversations are chat sessions between users and agents.
	//
	// Returns a slice of Conversation pointers on success, or a merrors.Error on failure.
	ListConversations(ctx context.Context, opts *ListConversationsOpts) ([]*orchestrator.Conversation, *merrors.Error)

	// GetConversation retrieves a specific conversation by its ID, ensuring
	// the user has access to that conversation's workspace.
	//
	// Returns the Conversation on success, or a merrors.Error if not found or on failure.
	GetConversation(ctx context.Context, opts *GetConversationOpts) (*orchestrator.Conversation, *merrors.Error)

	// ListMessages retrieves paginated messages from a conversation. Supports
	// cursor-based pagination for efficient scrolling through message history.
	//
	// Returns a MessagePage containing the messages and pagination info on success,
	// or a merrors.Error on failure.
	ListMessages(ctx context.Context, opts *ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error)

	// SendMessage creates a new message in a conversation. Uses LocalID for
	// idempotency, allowing clients to safely retry on network failures.
	// Returns the agent reply messages (one per agent turn) on success.
	//
	// Returns the created Messages on success, or a merrors.Error on failure.
	SendMessage(ctx context.Context, opts *SendMessageOpts) ([]*orchestrator.Message, *merrors.Error)

	// GetWorkspaceSummary retrieves aggregate workspace data including agent count
	// and the most recent message preview. The caller must verify workspace ownership
	// before calling this method.
	//
	// Returns a WorkspaceSummary on success, or a merrors.Error on failure.
	GetWorkspaceSummary(ctx context.Context, opts *GetWorkspaceSummaryOpts) (*orchestrator.WorkspaceSummary, *merrors.Error)

	// StartPendingMessageCleanup launches a background goroutine that periodically
	// marks stale pending messages as failed. The goroutine stops when ctx is cancelled.
	// Call this once at server startup.
	StartPendingMessageCleanup(ctx context.Context)
}

// AgentService defines the interface for agent-specific operations.
// Handles agent details, settings, tools, and history retrieval.
type AgentService interface {
	// GetAgent retrieves a single agent by ID with runtime status enrichment.
	// Verifies the requesting user owns the agent's workspace.
	//
	// Returns the Agent on success, or a merrors.Error on failure.
	GetAgent(ctx context.Context, opts *GetAgentOpts) (*orchestrator.Agent, *merrors.Error)

	// GetAgentDetails retrieves full agent details including stats.
	// Verifies the requesting user owns the agent's workspace.
	//
	// Returns AgentDetails on success, or a merrors.Error on failure.
	GetAgentDetails(ctx context.Context, opts *GetAgentDetailsOpts) (*orchestrator.AgentDetails, *merrors.Error)

	// GetAgentHistory retrieves paginated conversation history for an agent.
	// Verifies the requesting user owns the agent's workspace.
	//
	// Returns a slice of AgentHistoryItem with pagination on success, or a merrors.Error on failure.
	GetAgentHistory(ctx context.Context, opts *GetAgentHistoryOpts) ([]orchestrator.AgentHistoryItem, *orchestrator.OffsetPagination, *merrors.Error)

	// GetAgentSettings retrieves model and prompt settings for an agent.
	// Verifies the requesting user owns the agent's workspace.
	//
	// Returns AgentSettings on success, or a merrors.Error on failure.
	GetAgentSettings(ctx context.Context, opts *GetAgentSettingsOpts) (*orchestrator.AgentSettings, *merrors.Error)

	// GetAgentTools retrieves the tools assigned to an agent with pagination.
	// Verifies the requesting user owns the agent's workspace.
	//
	// Returns a ToolPage on success, or a merrors.Error on failure.
	GetAgentTools(ctx context.Context, opts *GetAgentToolsOpts) (*orchestrator.ToolPage, *merrors.Error)

	// GetAgentMemories retrieves memories from the agent runtime.
	GetAgentMemories(ctx context.Context, opts *GetAgentMemoriesOpts) ([]AgentMemory, *merrors.Error)

	// DeleteAgentMemory removes a specific memory from the agent runtime.
	DeleteAgentMemory(ctx context.Context, opts *DeleteAgentMemoryOpts) *merrors.Error

	// CreateAgentMemory stores a new memory in the agent runtime.
	CreateAgentMemory(ctx context.Context, opts *CreateAgentMemoryOpts) *merrors.Error
}

// IntegrationService manages third-party app connections via OAuth.
// Handles listing available integrations, initiating OAuth flows,
// and exchanging authorization codes for tokens.
type IntegrationService interface {
	// ListIntegrations returns all available integrations with the user's
	// connection status for each provider.
	//
	// Returns a slice of IntegrationItem pointers on success, or a merrors.Error on failure.
	ListIntegrations(ctx context.Context, opts *ListIntegrationsOpts) ([]*orchestrator.IntegrationItem, *merrors.Error)

	// GetOAuthConfig returns the OAuth configuration for initiating an
	// authorization flow with the specified provider.
	//
	// Returns the OAuthConfig on success, or a merrors.Error on failure.
	GetOAuthConfig(ctx context.Context, opts *GetOAuthConfigOpts) (*orchestrator.OAuthConfig, *merrors.Error)

	// HandleOAuthCallback exchanges the authorization code for tokens
	// and stores the connection in the database.
	//
	// Returns a merrors.Error if the operation fails, or nil on success.
	HandleOAuthCallback(ctx context.Context, opts *OAuthCallbackOpts) *merrors.Error
}
