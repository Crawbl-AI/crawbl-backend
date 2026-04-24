// Package repo provides repository interfaces and database row types for the orchestrator layer.
// It defines the data access contracts and persistence row mappings for users, workspaces,
// agents, conversations, and messages.
package repo

import (
	"encoding/json"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
)

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

// NewUserPushTokenRow creates a UserPushTokenRow with the given parameters.
func NewUserPushTokenRow(userID, pushToken string, updatedAt time.Time) *UserPushTokenRow {
	return &UserPushTokenRow{
		UserID:    userID,
		PushToken: pushToken,
		UpdatedAt: &updatedAt,
	}
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
// Status is not a persisted column — it's a transient activity state
// (online, thinking, writing, …) that arrives via agent.status socket
// events. Defaulting here to AgentStatusOffline guarantees every
// repo-loaded agent carries a valid enum value; a live event can
// still overwrite it. Without this default the zero-value empty
// string reaches the wire on message.new payloads and crashes the
// mobile AgentStatus enum parser.
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
		Status:       orchestrator.AgentStatusOffline,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
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
