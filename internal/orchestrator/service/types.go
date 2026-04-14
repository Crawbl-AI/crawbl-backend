// Package service defines the shared opts/DTO structs used across the
// orchestrator's service implementations. Service contracts live at each
// consumer (see server/ports.go, server/handler/ports.go, server/socketio/types.go)
// rather than here, per the project's "interfaces at consumer" convention.
//
// Each service operation uses a typed options struct (suffixed with "Opts") that
// encapsulates all required parameters, keeping call sites readable and making
// new optional parameters a one-line addition.
package service

import (
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
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
	// Title is an optional display title for the conversation.
	Title string
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
