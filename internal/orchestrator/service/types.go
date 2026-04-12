// Package service defines the service layer contracts and options types for the
// orchestrator domain. It provides typed interfaces for authentication, workspace
// management, and chat operations, along with their corresponding options structs.
//
// The service layer sits between the HTTP handlers (server package) and the
// repository layer (repo package), implementing business logic and orchestrating
// cross-cutting concerns like transactions, validation, and error handling.
//
// Each service operation uses a typed options struct (suffixed with "Opts") that
// encapsulates all required parameters, following a consistent pattern for
// dependency injection and testability.
package service

import (
	"context"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// SignUpOpts contains the parameters required for user registration.
// This struct is used when creating a new user account in the system.
type SignUpOpts struct {
	// Principal contains the authentication credentials and identity information
	// for the user being created.
	Principal *orchestrator.Principal
}

// SignInOpts contains the parameters required for user authentication.
// This struct is used when authenticating an existing user.
type SignInOpts struct {
	// Principal contains the authentication credentials to validate.
	Principal *orchestrator.Principal
}

// DeleteOpts contains the parameters required for user account deletion.
// This struct supports both soft-delete semantics and audit trail requirements.
type DeleteOpts struct {
	// Principal identifies the user account to be deleted.
	Principal *orchestrator.Principal
	// Reason provides a categorization for the deletion (e.g., "user_request", "admin_action").
	Reason string
	// Description provides additional context about the deletion, such as
	// user feedback or administrative notes.
	Description string
}

// GetUserBySubjectOpts contains the parameters for retrieving a user by their
// external identity subject identifier (e.g., Firebase UID, OAuth provider ID).
type GetUserBySubjectOpts struct {
	// Subject is the unique external identifier for the user, typically provided
	// by the authentication provider (Firebase Auth, OAuth, etc.).
	Subject string
}

// UpdateProfileOpts contains the parameters for updating user profile information.
// All profile fields are optional pointers; only non-nil fields will be updated.
type UpdateProfileOpts struct {
	// Principal identifies the user whose profile is being updated.
	Principal *orchestrator.Principal
	// Nickname is the user's display name or handle. Set to nil to leave unchanged.
	Nickname *string
	// Name is the user's given name. Set to nil to leave unchanged.
	Name *string
	// Surname is the user's family name. Set to nil to leave unchanged.
	Surname *string
	// CountryCode is the ISO 3166-1 alpha-2 country code for the user's location.
	// Set to nil to leave unchanged.
	CountryCode *string
	// DateOfBirth is the user's birth date. Set to nil to leave unchanged.
	DateOfBirth *time.Time
	// Preferences contains user-specific settings like notification preferences,
	// theme choices, and other customization options. Set to nil to leave unchanged.
	Preferences *orchestrator.UserPreferences
}

// AcceptLegalOpts contains the parameters for recording user acceptance of
// legal documents (terms of service, privacy policy).
type AcceptLegalOpts struct {
	// Principal identifies the user accepting the legal documents.
	Principal *orchestrator.Principal
	// TermsOfServiceVersion is the specific version of the terms of service
	// being accepted. Set to nil if only accepting the privacy policy.
	TermsOfServiceVersion *string
	// PrivacyPolicyVersion is the specific version of the privacy policy
	// being accepted. Set to nil if only accepting the terms of service.
	PrivacyPolicyVersion *string
}

// SavePushTokenOpts contains the parameters for registering a push notification
// token for a user's device.
type SavePushTokenOpts struct {
	// Principal identifies the user whose push token is being registered.
	Principal *orchestrator.Principal
	// PushToken is the FCM (Firebase Cloud Messaging) or APNs (Apple Push
	// Notification service) token for the user's device.
	PushToken string
}

// EnsureDefaultWorkspaceOpts contains the parameters for creating the default
// workspace for a user. This is typically called during user provisioning.
type EnsureDefaultWorkspaceOpts struct {
	// UserID is the unique identifier of the user who will own the default workspace.
	UserID string
}

// ListWorkspacesOpts contains the parameters for listing all workspaces
// associated with a specific user.
type ListWorkspacesOpts struct {
	// UserID is the unique identifier of the user whose workspaces are being listed.
	UserID string
}

// GetWorkspaceOpts contains the parameters for retrieving a specific workspace
// by its ID, scoped to a user.
type GetWorkspaceOpts struct {
	// UserID is the unique identifier of the user who owns the workspace.
	UserID string
	// WorkspaceID is the unique identifier of the workspace to retrieve.
	WorkspaceID string
}

// ListAgentsOpts contains the parameters for listing all agents available
// within a specific workspace.
type ListAgentsOpts struct {
	// UserID is the unique identifier of the user requesting the agent list.
	UserID string
	// WorkspaceID is the unique identifier of the workspace containing the agents.
	WorkspaceID string
}

// ListConversationsOpts contains the parameters for listing all conversations
// within a specific workspace.
type ListConversationsOpts struct {
	// UserID is the unique identifier of the user requesting the conversation list.
	UserID string
	// WorkspaceID is the unique identifier of the workspace containing the conversations.
	WorkspaceID string
}

// GetConversationOpts contains the parameters for retrieving a specific conversation
// by its ID, scoped to a workspace.
type GetConversationOpts struct {
	// UserID is the unique identifier of the user requesting the conversation.
	UserID string
	// WorkspaceID is the unique identifier of the workspace containing the conversation.
	WorkspaceID string
	// ConversationID is the unique identifier of the conversation to retrieve.
	ConversationID string
}

// ListMessagesOpts contains the parameters for paginated message retrieval
// within a conversation.
type ListMessagesOpts struct {
	// UserID is the unique identifier of the user requesting the messages.
	UserID string
	// WorkspaceID is the unique identifier of the workspace containing the conversation.
	WorkspaceID string
	// ConversationID is the unique identifier of the conversation containing the messages.
	ConversationID string
	// ScrollID is a cursor for pagination, representing the position in the
	// message history from which to start fetching.
	ScrollID string
	// Limit specifies the maximum number of messages to return in a single request.
	Limit int
	// Direction indicates the pagination direction: "before" or "after" the ScrollID.
	Direction string
}

// SendMessageOpts contains the parameters for sending a new message to a conversation.
type SendMessageOpts struct {
	// UserID is the unique identifier of the user sending the message.
	UserID string
	// WorkspaceID is the unique identifier of the workspace containing the conversation.
	WorkspaceID string
	// ConversationID is the unique identifier of the conversation to send the message to.
	ConversationID string
	// LocalID is the client-generated identifier for idempotency, allowing
	// clients to retry message sends without creating duplicates.
	LocalID string
	// Content is the message body, supporting various content types (text, markdown, etc.).
	Content orchestrator.MessageContent
	// Attachments is the list of files or resources attached to the message.
	Attachments []orchestrator.Attachment
	// Mentions is the list of @-mentioned agents in the message (swarm chat).
	Mentions []orchestrator.Mention
	// OnPersisted is called after the user message is saved to DB, before agent processing.
	// Used by the transport layer to emit early acknowledgement to the sender.
	OnPersisted func(userMsg *orchestrator.Message)
}

// ClearPushTokenOpts contains the parameters for clearing all push tokens for a user.
type ClearPushTokenOpts struct {
	// UserID is the unique identifier of the user whose tokens are being cleared.
	UserID string
}

// CreateConversationOpts contains the parameters for creating a new conversation.
type CreateConversationOpts struct {
	// UserID is the unique identifier of the user creating the conversation.
	UserID string
	// WorkspaceID is the unique identifier of the workspace for the conversation.
	WorkspaceID string
	// Type indicates whether this is a swarm or single-agent conversation.
	Type orchestrator.ConversationType
	// AgentID is required for agent-type conversations.
	AgentID string
}

// DeleteConversationOpts contains the parameters for deleting a conversation.
type DeleteConversationOpts struct {
	// UserID is the unique identifier of the user requesting the deletion.
	UserID string
	// WorkspaceID is the unique identifier of the workspace owning the conversation.
	WorkspaceID string
	// ConversationID is the unique identifier of the conversation to delete.
	ConversationID string
}

// MarkConversationReadOpts contains the parameters for marking a conversation as read.
type MarkConversationReadOpts struct {
	// UserID is the unique identifier of the user marking the conversation read.
	UserID string
	// WorkspaceID is the unique identifier of the workspace owning the conversation.
	WorkspaceID string
	// ConversationID is the unique identifier of the conversation to mark as read.
	ConversationID string
}

// RespondToActionCardOpts contains the parameters for responding to an action card message.
type RespondToActionCardOpts struct {
	// UserID is the unique identifier of the user responding.
	UserID string
	// WorkspaceID is the unique identifier of the workspace owning the message.
	WorkspaceID string
	// MessageID is the unique identifier of the action card message.
	MessageID string
	// ActionID is the ID of the selected action item.
	ActionID string
}

// GetWorkspaceSummaryOpts contains options for the GetWorkspaceSummary method.
type GetWorkspaceSummaryOpts struct {
	// WorkspaceID is the unique identifier of the workspace to summarize.
	WorkspaceID string
}

// GetAgentOpts contains the parameters for retrieving a single agent by ID.
type GetAgentOpts struct {
	// UserID is the unique identifier of the requesting user.
	UserID string
	// AgentID is the unique identifier of the agent to retrieve.
	AgentID string
}

// GetAgentDetailsOpts contains the parameters for retrieving full agent details.
type GetAgentDetailsOpts struct {
	// UserID is the unique identifier of the requesting user.
	UserID string
	// AgentID is the unique identifier of the agent to retrieve.
	AgentID string
}

// GetAgentHistoryOpts contains the parameters for retrieving agent history.
type GetAgentHistoryOpts struct {
	// UserID is the unique identifier of the requesting user.
	UserID string
	// AgentID is the unique identifier of the agent.
	AgentID string
	// Limit specifies the maximum number of history items to return.
	Limit int
	// Offset specifies the number of items to skip for pagination.
	Offset int
}

// GetAgentSettingsOpts contains the parameters for retrieving agent settings.
type GetAgentSettingsOpts struct {
	// UserID is the unique identifier of the requesting user.
	UserID string
	// AgentID is the unique identifier of the agent.
	AgentID string
}

// GetAgentToolsOpts contains the parameters for retrieving an agent's tools.
type GetAgentToolsOpts struct {
	// UserID is the unique identifier of the requesting user.
	UserID string
	// AgentID is the unique identifier of the agent.
	AgentID string
	// Limit specifies the maximum number of tools to return.
	Limit int
	// Offset specifies the number of tools to skip for pagination.
	Offset int
}

// GetAgentMemoriesOpts contains the parameters for retrieving agent memories.
type GetAgentMemoriesOpts struct {
	UserID   string
	AgentID  string
	Category string
	Limit    int
	Offset   int
}

// DeleteAgentMemoryOpts contains the parameters for deleting an agent memory.
type DeleteAgentMemoryOpts struct {
	UserID  string
	AgentID string
	Key     string
}

// CreateAgentMemoryOpts contains the parameters for creating an agent memory.
type CreateAgentMemoryOpts struct {
	UserID   string
	AgentID  string
	Key      string
	Content  string
	Category string
}

// AgentMemory represents a memory entry from the agent runtime.
type AgentMemory struct {
	Key       string
	Content   string
	Category  string
	CreatedAt string
	UpdatedAt string
}

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

	// ClearPushToken removes all push notification tokens for a user's devices.
	// This is called on logout so the user stops receiving push notifications.
	//
	// Returns a merrors.Error if the operation fails, or nil on success.
	ClearPushToken(ctx context.Context, opts *ClearPushTokenOpts) *merrors.Error
}

// WorkspaceService defines the interface for workspace management operations.
// Implementations handle workspace creation, listing, and retrieval.
type WorkspaceService interface {
	// EnsureDefaultWorkspace creates the default workspace for a user if it does
	// not already exist. This is typically called during user provisioning and
	// may trigger Kubernetes UserSwarm resource creation.
	//
	// Returns a merrors.Error if the operation fails, or nil on success.
	EnsureDefaultWorkspace(ctx context.Context, opts *EnsureDefaultWorkspaceOpts) *merrors.Error

	// ListByUserID retrieves all workspaces associated with a specific user.
	// The returned workspaces may include runtime status information from the
	// associated UserSwarm resources.
	//
	// Returns a slice of Workspace pointers on success, or a merrors.Error on failure.
	ListByUserID(ctx context.Context, opts *ListWorkspacesOpts) ([]*orchestrator.Workspace, *merrors.Error)

	// GetByID retrieves a specific workspace by its ID, ensuring the user has
	// access to that workspace.
	//
	// Returns the Workspace on success, or a merrors.Error if not found or on failure.
	GetByID(ctx context.Context, opts *GetWorkspaceOpts) (*orchestrator.Workspace, *merrors.Error)
}

// ChatService defines the interface for all chat operations: conversations,
// messages, agents, and streaming. Handlers depend on this interface directly.
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

	// GetWorkspaceSummary retrieves aggregate workspace data including agent count
	// and the most recent message preview. The caller must verify workspace ownership
	// before calling this method.
	//
	// Returns a WorkspaceSummary on success, or a merrors.Error on failure.
	GetWorkspaceSummary(ctx context.Context, opts *GetWorkspaceSummaryOpts) (*orchestrator.WorkspaceSummary, *merrors.Error)

	// SendMessage creates a new message in a conversation. Uses LocalID for
	// idempotency, allowing clients to safely retry on network failures.
	// Returns the agent reply messages (one per agent turn) on success.
	//
	// Returns the created Messages on success, or a merrors.Error on failure.
	SendMessage(ctx context.Context, opts *SendMessageOpts) ([]*orchestrator.Message, *merrors.Error)

	// CreateConversation creates a new conversation within a workspace.
	//
	// Returns the created Conversation on success, or a merrors.Error on failure.
	CreateConversation(ctx context.Context, opts *CreateConversationOpts) (*orchestrator.Conversation, *merrors.Error)

	// DeleteConversation removes a conversation from a workspace.
	//
	// Returns a merrors.Error if the operation fails, or nil on success.
	DeleteConversation(ctx context.Context, opts *DeleteConversationOpts) *merrors.Error

	// MarkConversationRead resets the unread count for a conversation to zero.
	//
	// Returns a merrors.Error if the operation fails, or nil on success.
	MarkConversationRead(ctx context.Context, opts *MarkConversationReadOpts) *merrors.Error

	// RespondToActionCard records the user's selection for an action card message.
	//
	// Returns the updated Message on success, or a merrors.Error on failure.
	RespondToActionCard(ctx context.Context, opts *RespondToActionCardOpts) (*orchestrator.Message, *merrors.Error)
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

	// GetAgentMemories retrieves memories from the agent's agent runtime.
	GetAgentMemories(ctx context.Context, opts *GetAgentMemoriesOpts) ([]AgentMemory, *merrors.Error)

	// DeleteAgentMemory removes a specific memory from the agent's agent runtime.
	DeleteAgentMemory(ctx context.Context, opts *DeleteAgentMemoryOpts) *merrors.Error

	// CreateAgentMemory stores a new memory in the agent's agent runtime.
	CreateAgentMemory(ctx context.Context, opts *CreateAgentMemoryOpts) *merrors.Error
}

// ListIntegrationsOpts contains parameters for listing available integrations.
type ListIntegrationsOpts struct {
	// UserID is the unique identifier of the user requesting the integration list.
	UserID string
}

// GetOAuthConfigOpts contains parameters for getting OAuth config for a provider.
type GetOAuthConfigOpts struct {
	// UserID is the unique identifier of the user requesting the OAuth config.
	UserID string
	// Provider is the integration provider identifier (e.g., "google_calendar", "gmail").
	Provider string
}

// OAuthCallbackOpts contains the OAuth callback parameters from the mobile app.
type OAuthCallbackOpts struct {
	// UserID is the unique identifier of the user completing the OAuth flow.
	UserID string
	// Provider is the integration provider identifier.
	Provider string
	// AuthorizationCode is the OAuth authorization code from the provider.
	AuthorizationCode string
	// CodeVerifier is the PKCE code verifier used to initiate the flow.
	CodeVerifier string
	// RedirectURL is the redirect URI used in the OAuth flow.
	RedirectURL string
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
