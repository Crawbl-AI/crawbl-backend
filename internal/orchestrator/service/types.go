package service

import (
	"context"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"

	"github.com/gocraft/dbr/v2"
)

type SignUpOpts struct {
	Sess      *dbr.Session
	Principal *orchestrator.Principal
}

type SignInOpts struct {
	Sess      *dbr.Session
	Principal *orchestrator.Principal
}

type DeleteOpts struct {
	Sess        *dbr.Session
	Principal   *orchestrator.Principal
	Reason      string
	Description string
}

type GetUserBySubjectOpts struct {
	Sess    orchestratorrepo.SessionRunner
	Subject string
}

type UpdateProfileOpts struct {
	Sess        *dbr.Session
	Principal   *orchestrator.Principal
	Nickname    *string
	Name        *string
	Surname     *string
	CountryCode *string
	DateOfBirth *time.Time
	Preferences *orchestrator.UserPreferences
}

type AcceptLegalOpts struct {
	Sess                  *dbr.Session
	Principal             *orchestrator.Principal
	TermsOfServiceVersion *string
	PrivacyPolicyVersion  *string
}

type SavePushTokenOpts struct {
	Sess      *dbr.Session
	Principal *orchestrator.Principal
	PushToken string
}

type EnsureDefaultWorkspaceOpts struct {
	Sess   orchestratorrepo.SessionRunner
	UserID string
}

type ListWorkspacesOpts struct {
	Sess   orchestratorrepo.SessionRunner
	UserID string
}

type GetWorkspaceOpts struct {
	Sess        orchestratorrepo.SessionRunner
	UserID      string
	WorkspaceID string
}

type ListAgentsOpts struct {
	Sess        *dbr.Session
	UserID      string
	WorkspaceID string
}

type ListConversationsOpts struct {
	Sess        *dbr.Session
	UserID      string
	WorkspaceID string
}

type GetConversationOpts struct {
	Sess           *dbr.Session
	UserID         string
	WorkspaceID    string
	ConversationID string
}

type ListMessagesOpts struct {
	Sess           *dbr.Session
	UserID         string
	WorkspaceID    string
	ConversationID string
	ScrollID       string
	Limit          int
	Direction      string
}

type SendMessageOpts struct {
	Sess           *dbr.Session
	UserID         string
	WorkspaceID    string
	ConversationID string
	LocalID        string
	Content        orchestrator.MessageContent
	Attachments    []orchestrator.Attachment
}

type WorkspaceBootstrapper interface {
	EnsureDefaultWorkspace(ctx context.Context, opts *EnsureDefaultWorkspaceOpts) *merrors.Error
}

type AuthService interface {
	SignUp(ctx context.Context, opts *SignUpOpts) (*orchestrator.User, *merrors.Error)
	SignIn(ctx context.Context, opts *SignInOpts) (*orchestrator.User, *merrors.Error)
	Delete(ctx context.Context, opts *DeleteOpts) *merrors.Error
	GetBySubject(ctx context.Context, opts *GetUserBySubjectOpts) (*orchestrator.User, *merrors.Error)
	UpdateProfile(ctx context.Context, opts *UpdateProfileOpts) (*orchestrator.User, *merrors.Error)
	GetLegalDocuments(ctx context.Context) (*orchestrator.LegalDocuments, *merrors.Error)
	AcceptLegal(ctx context.Context, opts *AcceptLegalOpts) (*orchestrator.User, *merrors.Error)
	SavePushToken(ctx context.Context, opts *SavePushTokenOpts) *merrors.Error
}

type WorkspaceService interface {
	EnsureDefaultWorkspace(ctx context.Context, opts *EnsureDefaultWorkspaceOpts) *merrors.Error
	ListByUserID(ctx context.Context, opts *ListWorkspacesOpts) ([]*orchestrator.Workspace, *merrors.Error)
	GetByID(ctx context.Context, opts *GetWorkspaceOpts) (*orchestrator.Workspace, *merrors.Error)
}

type ChatService interface {
	ListAgents(ctx context.Context, opts *ListAgentsOpts) ([]*orchestrator.Agent, *merrors.Error)
	ListConversations(ctx context.Context, opts *ListConversationsOpts) ([]*orchestrator.Conversation, *merrors.Error)
	GetConversation(ctx context.Context, opts *GetConversationOpts) (*orchestrator.Conversation, *merrors.Error)
	ListMessages(ctx context.Context, opts *ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error)
	SendMessage(ctx context.Context, opts *SendMessageOpts) (*orchestrator.Message, *merrors.Error)
}
