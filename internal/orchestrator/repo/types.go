package repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

type SessionRunner = database.SessionRunner

func Columns(columns ...string) []interface{} {
	converted := make([]interface{}, 0, len(columns))
	for _, column := range columns {
		converted = append(converted, column)
	}

	return converted
}

type UserRow struct {
	ID                         string     `db:"id"`
	Subject                    string     `db:"subject"`
	Email                      string     `db:"email"`
	Nickname                   string     `db:"nickname"`
	Name                       string     `db:"name"`
	Surname                    string     `db:"surname"`
	AvatarURL                  *string    `db:"avatar_url"`
	CountryCode                *string    `db:"country_code"`
	DateOfBirth                *time.Time `db:"date_of_birth"`
	IsBanned                   bool       `db:"is_banned"`
	HasAgreedWithTerms         bool       `db:"has_agreed_with_terms"`
	HasAgreedWithPrivacyPolicy bool       `db:"has_agreed_with_privacy_policy"`
	CreatedAt                  time.Time  `db:"created_at"`
	UpdatedAt                  time.Time  `db:"updated_at"`
	DeletedAt                  *time.Time `db:"deleted_at"`
}

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

type UserPreferencesRow struct {
	UserID           string     `db:"user_id"`
	PlatformTheme    *string    `db:"platform_theme"`
	PlatformLanguage *string    `db:"platform_language"`
	CurrencyCode     *string    `db:"currency_code"`
	UpdatedAt        *time.Time `db:"updated_at"`
}

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

func (r *UserPreferencesRow) ApplyToUser(user *orchestrator.User) {
	if user == nil || r == nil {
		return
	}

	user.Preferences.PlatformTheme = r.PlatformTheme
	user.Preferences.PlatformLanguage = r.PlatformLanguage
	user.Preferences.CurrencyCode = r.CurrencyCode
}

type UserPushTokenRow struct {
	UserID    string     `db:"user_id"`
	PushToken string     `db:"push_token"`
	UpdatedAt *time.Time `db:"updated_at"`
}

func NewUserPushTokenRow(userID, pushToken string, updatedAt time.Time) *UserPushTokenRow {
	return &UserPushTokenRow{
		UserID:    userID,
		PushToken: pushToken,
		UpdatedAt: &updatedAt,
	}
}

type WorkspaceRow struct {
	ID        string    `db:"id"`
	UserID    string    `db:"user_id"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

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

func (r *WorkspaceRow) ToDomain() *orchestrator.Workspace {
	return &orchestrator.Workspace{
		ID:        r.ID,
		UserID:    r.UserID,
		Name:      r.Name,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

type AgentRow struct {
	ID          string    `db:"id"`
	WorkspaceID string    `db:"workspace_id"`
	Name        string    `db:"name"`
	Role        string    `db:"role"`
	AvatarURL   string    `db:"avatar_url"`
	SortOrder   int       `db:"sort_order"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

func NewAgentRow(agent *orchestrator.Agent, sortOrder int) *AgentRow {
	if agent == nil {
		return nil
	}

	return &AgentRow{
		ID:          agent.ID,
		WorkspaceID: agent.WorkspaceID,
		Name:        agent.Name,
		Role:        agent.Role,
		AvatarURL:   agent.AvatarURL,
		SortOrder:   sortOrder,
		CreatedAt:   agent.CreatedAt,
		UpdatedAt:   agent.UpdatedAt,
	}
}

func (r *AgentRow) ToDomain() *orchestrator.Agent {
	return &orchestrator.Agent{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		Role:        r.Role,
		AvatarURL:   r.AvatarURL,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

type ConversationRow struct {
	ID          string    `db:"id"`
	WorkspaceID string    `db:"workspace_id"`
	AgentID     *string   `db:"agent_id"`
	Type        string    `db:"type"`
	Title       string    `db:"title"`
	UnreadCount int       `db:"unread_count"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

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

type MessageRow struct {
	ID             string          `db:"id"`
	ConversationID string          `db:"conversation_id"`
	Role           string          `db:"role"`
	Content        json.RawMessage `db:"content"`
	Status         string          `db:"status"`
	LocalID        *string         `db:"local_id"`
	AgentID        *string         `db:"agent_id"`
	Attachments    json.RawMessage `db:"attachments"`
	CreatedAt      time.Time       `db:"created_at"`
	UpdatedAt      time.Time       `db:"updated_at"`
}

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

type UserRepo interface {
	GetBySubject(ctx context.Context, sess SessionRunner, subject string) (*orchestrator.User, *merrors.Error)
	Save(ctx context.Context, sess SessionRunner, user *orchestrator.User) *merrors.Error
	SavePushToken(ctx context.Context, sess SessionRunner, userID, pushToken string) *merrors.Error
}

type WorkspaceRepo interface {
	ListByUserID(ctx context.Context, sess SessionRunner, userID string) ([]*orchestrator.Workspace, *merrors.Error)
	GetByID(ctx context.Context, sess SessionRunner, userID, workspaceID string) (*orchestrator.Workspace, *merrors.Error)
	Save(ctx context.Context, sess SessionRunner, workspace *orchestrator.Workspace) *merrors.Error
}

type AgentRepo interface {
	ListByWorkspaceID(ctx context.Context, sess SessionRunner, workspaceID string) ([]*orchestrator.Agent, *merrors.Error)
	GetByID(ctx context.Context, sess SessionRunner, workspaceID, agentID string) (*orchestrator.Agent, *merrors.Error)
	Save(ctx context.Context, sess SessionRunner, agent *orchestrator.Agent, sortOrder int) *merrors.Error
}

type ConversationRepo interface {
	ListByWorkspaceID(ctx context.Context, sess SessionRunner, workspaceID string) ([]*orchestrator.Conversation, *merrors.Error)
	GetByID(ctx context.Context, sess SessionRunner, workspaceID, conversationID string) (*orchestrator.Conversation, *merrors.Error)
	FindDefaultSwarm(ctx context.Context, sess SessionRunner, workspaceID string) (*orchestrator.Conversation, *merrors.Error)
	Save(ctx context.Context, sess SessionRunner, conversation *orchestrator.Conversation) *merrors.Error
}

type ListMessagesOpts struct {
	ConversationID string
	ScrollID       string
	Limit          int
}

type MessageRepo interface {
	ListByConversationID(ctx context.Context, sess SessionRunner, opts *ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error)
	GetLatestByConversationID(ctx context.Context, sess SessionRunner, conversationID string) (*orchestrator.Message, *merrors.Error)
	Save(ctx context.Context, sess SessionRunner, message *orchestrator.Message) *merrors.Error
}
