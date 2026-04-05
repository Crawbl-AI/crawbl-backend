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
//
// Service interface definitions live in interfaces.go; this file contains only
// the Opts structs and domain types used as service-layer contracts.
package service

import (
	"sync"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"

	"github.com/gocraft/dbr/v2"
)

// SignUpOpts contains the parameters required for user registration.
// This struct is used when creating a new user account in the system.
type SignUpOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// Principal contains the authentication credentials and identity information
	// for the user being created.
	Principal *orchestrator.Principal
}

// SignInOpts contains the parameters required for user authentication.
// This struct is used when authenticating an existing user.
type SignInOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// Principal contains the authentication credentials to validate.
	Principal *orchestrator.Principal
}

// DeleteOpts contains the parameters required for user account deletion.
// This struct supports both soft-delete semantics and audit trail requirements.
type DeleteOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
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
	// Sess is the database session or transaction runner. Accepts either a direct
	// session or a transaction-wrapped runner for flexibility.
	Sess orchestratorrepo.SessionRunner
	// Subject is the unique external identifier for the user, typically provided
	// by the authentication provider (Firebase Auth, OAuth, etc.).
	Subject string
}

// UpdateProfileOpts contains the parameters for updating user profile information.
// All profile fields are optional pointers; only non-nil fields will be updated.
type UpdateProfileOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
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
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
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
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// Principal identifies the user whose push token is being registered.
	Principal *orchestrator.Principal
	// PushToken is the FCM (Firebase Cloud Messaging) or APNs (Apple Push
	// Notification service) token for the user's device.
	PushToken string
}

// EnsureDefaultWorkspaceOpts contains the parameters for creating the default
// workspace for a user. This is typically called during user provisioning.
type EnsureDefaultWorkspaceOpts struct {
	// Sess is the database session or transaction runner. Accepts either a direct
	// session or a transaction-wrapped runner for flexibility.
	Sess orchestratorrepo.SessionRunner
	// UserID is the unique identifier of the user who will own the default workspace.
	UserID string
}

// ListWorkspacesOpts contains the parameters for listing all workspaces
// associated with a specific user.
type ListWorkspacesOpts struct {
	// Sess is the database session or transaction runner. Accepts either a direct
	// session or a transaction-wrapped runner for flexibility.
	Sess orchestratorrepo.SessionRunner
	// UserID is the unique identifier of the user whose workspaces are being listed.
	UserID string
}

// GetWorkspaceOpts contains the parameters for retrieving a specific workspace
// by its ID, scoped to a user.
type GetWorkspaceOpts struct {
	// Sess is the database session or transaction runner. Accepts either a direct
	// session or a transaction-wrapped runner for flexibility.
	Sess orchestratorrepo.SessionRunner
	// UserID is the unique identifier of the user who owns the workspace.
	UserID string
	// WorkspaceID is the unique identifier of the workspace to retrieve.
	WorkspaceID string
}

// ListAgentsOpts contains the parameters for listing all agents available
// within a specific workspace.
type ListAgentsOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// UserID is the unique identifier of the user requesting the agent list.
	UserID string
	// WorkspaceID is the unique identifier of the workspace containing the agents.
	WorkspaceID string
}

// ListConversationsOpts contains the parameters for listing all conversations
// within a specific workspace.
type ListConversationsOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// UserID is the unique identifier of the user requesting the conversation list.
	UserID string
	// WorkspaceID is the unique identifier of the workspace containing the conversations.
	WorkspaceID string
}

// GetConversationOpts contains the parameters for retrieving a specific conversation
// by its ID, scoped to a workspace.
type GetConversationOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
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
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
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
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
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
	// UserMessageID is set internally after the user message is persisted.
	// Used downstream to emit status updates for the original user message.
	UserMessageID string
	// StatusDeliveredOnce ensures the "delivered" status is emitted only once
	// when multiple agents process the same user message in parallel.
	StatusDeliveredOnce *sync.Once
	// StatusReadOnce ensures the "read" status is emitted only once.
	StatusReadOnce *sync.Once
}

// GetWorkspaceSummaryOpts contains options for the GetWorkspaceSummary method.
type GetWorkspaceSummaryOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// WorkspaceID is the unique identifier of the workspace to summarize.
	WorkspaceID string
}

// GetAgentOpts contains the parameters for retrieving a single agent by ID.
type GetAgentOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// UserID is the unique identifier of the requesting user.
	UserID string
	// AgentID is the unique identifier of the agent to retrieve.
	AgentID string
}

// GetAgentDetailsOpts contains the parameters for retrieving full agent details.
type GetAgentDetailsOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// UserID is the unique identifier of the requesting user.
	UserID string
	// AgentID is the unique identifier of the agent to retrieve.
	AgentID string
}

// GetAgentHistoryOpts contains the parameters for retrieving agent history.
type GetAgentHistoryOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
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
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// UserID is the unique identifier of the requesting user.
	UserID string
	// AgentID is the unique identifier of the agent.
	AgentID string
}

// GetAgentToolsOpts contains the parameters for retrieving an agent's tools.
type GetAgentToolsOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
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
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// UserID is the unique identifier of the requesting user.
	UserID string
	// AgentID is the unique identifier of the agent whose memories are being retrieved.
	AgentID string
	// Category filters memories by category (e.g., "fact", "preference"). Empty means all categories.
	Category string
	// Limit specifies the maximum number of memories to return.
	Limit int
	// Offset specifies the number of memories to skip for pagination.
	Offset int
}

// DeleteAgentMemoryOpts contains the parameters for deleting an agent memory.
type DeleteAgentMemoryOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// UserID is the unique identifier of the requesting user.
	UserID string
	// AgentID is the unique identifier of the agent whose memory is being deleted.
	AgentID string
	// Key is the unique identifier of the memory entry to delete.
	Key string
}

// CreateAgentMemoryOpts contains the parameters for creating an agent memory.
type CreateAgentMemoryOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// UserID is the unique identifier of the requesting user.
	UserID string
	// AgentID is the unique identifier of the agent for whom the memory is being created.
	AgentID string
	// Key is the unique identifier for the memory entry within the agent's memory store.
	Key string
	// Content is the text content of the memory entry.
	Content string
	// Category groups the memory by type (e.g., "fact", "preference", "context").
	Category string
}

// AgentMemory represents a memory entry from the agent runtime.
type AgentMemory struct {
	Key       string `json:"key"`
	Content   string `json:"content"`
	Category  string `json:"category,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ListIntegrationsOpts contains parameters for listing available integrations.
type ListIntegrationsOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// UserID is the unique identifier of the user requesting the integration list.
	UserID string
}

// GetOAuthConfigOpts contains parameters for getting OAuth config for a provider.
type GetOAuthConfigOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
	// UserID is the unique identifier of the user requesting the OAuth config.
	UserID string
	// Provider is the integration provider identifier (e.g., "google_calendar", "gmail").
	Provider string
}

// OAuthCallbackOpts contains the OAuth callback parameters from the mobile app.
type OAuthCallbackOpts struct {
	// Sess is the database session for the transaction. Must be non-nil.
	Sess *dbr.Session
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
