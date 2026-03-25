package orchestrator

import (
	"context"
	"errors"
	"time"
)

const (
	DefaultDevTokenPrefix = "dev"
	DefaultWorkspaceName  = "My Swarm"
	DefaultSwarmTitle     = "My Swarm"
	DefaultAgentAvatarURL = ""
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrUnauthorized = errors.New("unauthorized")
	ErrUserNotFound = errors.New("user not found")
	ErrUserDeleted  = errors.New("user deleted")
	ErrNilPrincipal = errors.New("principal is required")
	ErrEmptySubject = errors.New("principal subject is required")
	ErrEmptyEmail   = errors.New("principal email is required")
	ErrNilUser      = errors.New("user is required")
)

type AgentStatus string

const (
	AgentStatusOnline  AgentStatus = "online"
	AgentStatusBusy    AgentStatus = "busy"
	AgentStatusOffline AgentStatus = "offline"
)

type ConversationType string

const (
	ConversationTypeSwarm ConversationType = "swarm"
	ConversationTypeAgent ConversationType = "agent"
)

type MessageRole string

const (
	MessageRoleUser   MessageRole = "user"
	MessageRoleAgent  MessageRole = "agent"
	MessageRoleSystem MessageRole = "system"
)

type MessageStatus string

const (
	MessageStatusPending   MessageStatus = "pending"
	MessageStatusDelivered MessageStatus = "delivered"
	MessageStatusFailed    MessageStatus = "failed"
)

type MessageContentType string

const (
	MessageContentTypeText       MessageContentType = "text"
	MessageContentTypeActionCard MessageContentType = "action_card"
	MessageContentTypeToolStatus MessageContentType = "tool_status"
	MessageContentTypeSystem     MessageContentType = "system"
	MessageContentTypeLoading    MessageContentType = "loading"
)

type ActionStyle string

const (
	ActionStylePrimary     ActionStyle = "primary"
	ActionStyleSecondary   ActionStyle = "secondary"
	ActionStyleDestructive ActionStyle = "destructive"
)

type ToolState string

const (
	ToolStateRunning   ToolState = "running"
	ToolStateCompleted ToolState = "completed"
	ToolStateFailed    ToolState = "failed"
)

type AttachmentType string

const (
	AttachmentTypeImage AttachmentType = "image"
	AttachmentTypeVideo AttachmentType = "video"
	AttachmentTypeAudio AttachmentType = "audio"
	AttachmentTypeFile  AttachmentType = "file"
)

type Principal struct {
	Subject string
	Email   string
	Name    string
}

type UserPreferences struct {
	PlatformTheme    *string
	PlatformLanguage *string
	CurrencyCode     *string
}

type UserSubscription struct {
	Name      string
	Code      string
	ExpiresAt *time.Time
}

type User struct {
	ID                         string
	Subject                    string
	Email                      string
	Nickname                   string
	Name                       string
	Surname                    string
	AvatarURL                  *string
	CountryCode                *string
	DateOfBirth                *time.Time
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
	DeletedAt                  *time.Time
	IsBanned                   bool
	HasAgreedWithTerms         bool
	HasAgreedWithPrivacyPolicy bool
	Preferences                UserPreferences
	Subscription               UserSubscription
}

type LegalDocuments struct {
	TermsOfService        string
	PrivacyPolicy         string
	TermsOfServiceVersion string
	PrivacyPolicyVersion  string
}

type Workspace struct {
	ID        string
	UserID    string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Runtime   *RuntimeStatus
}

type RuntimeState string

const (
	RuntimeStateProvisioning RuntimeState = "provisioning"
	RuntimeStateReady        RuntimeState = "ready"
	RuntimeStateOffline      RuntimeState = "offline"
	RuntimeStateFailed       RuntimeState = "failed"
)

type Agent struct {
	ID          string      `json:"id"`
	WorkspaceID string      `json:"workspaceId"`
	Name        string      `json:"name"`
	Role        string      `json:"role"`
	AvatarURL   string      `json:"avatar"`
	Status      AgentStatus `json:"status"`
	HasUpdate   bool        `json:"hasUpdate"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}

type Conversation struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspaceId"`
	AgentID     *string          `json:"-"`
	Type        ConversationType `json:"type"`
	Title       string           `json:"title"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
	UnreadCount int              `json:"unreadCount"`
	Agent       *Agent           `json:"agent,omitempty"`
	LastMessage *Message         `json:"lastMessage,omitempty"`
}

type Message struct {
	ID             string         `json:"id"`
	ConversationID string         `json:"conversationId"`
	Role           MessageRole    `json:"role"`
	Content        MessageContent `json:"content"`
	Status         MessageStatus  `json:"status"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
	LocalID        *string        `json:"localId,omitempty"`
	AgentID        *string        `json:"-"`
	Agent          *Agent         `json:"agent,omitempty"`
	Attachments    []Attachment   `json:"attachments"`
}

type MessageContent struct {
	Type             MessageContentType `json:"type"`
	Text             string             `json:"text,omitempty"`
	Title            string             `json:"title,omitempty"`
	Description      string             `json:"description,omitempty"`
	Actions          []ActionItem       `json:"actions,omitempty"`
	SelectedActionID *string            `json:"selectedActionId,omitempty"`
	Tool             string             `json:"tool,omitempty"`
	State            ToolState          `json:"state,omitempty"`
}

type ActionItem struct {
	ID    string      `json:"id"`
	Label string      `json:"label"`
	Style ActionStyle `json:"style"`
}

type Attachment struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	URL      string         `json:"url"`
	Type     AttachmentType `json:"type"`
	Size     int64          `json:"size"`
	MIMEType string         `json:"mimeType,omitempty"`
	Duration *int           `json:"duration,omitempty"`
}

type MessagePage struct {
	Data       []*Message
	Pagination Pagination
}

type Pagination struct {
	NextScrollID string
	PrevScrollID string
	HasNext      bool
	HasPrev      bool
}

type RuntimeStatus struct {
	SwarmName        string
	RuntimeNamespace string
	ServiceName      string
	Phase            string
	Verified         bool
	Status           RuntimeState
	LastError        string
}

type DefaultAgentBlueprint struct {
	Name string
	Role string
}

var DefaultAgents = []DefaultAgentBlueprint{
	{Name: "Research", Role: "researcher"},
	{Name: "Writer", Role: "writer"},
}

func ResolveRuntimeState(phase string, verified bool) RuntimeState {
	if verified {
		return RuntimeStateReady
	}

	switch phase {
	case "Pending", "Progressing", "Deleting", "":
		return RuntimeStateProvisioning
	case "Error":
		return RuntimeStateFailed
	case "Suspended":
		return RuntimeStateOffline
	default:
		return RuntimeStateOffline
	}
}

type IdentityVerifier interface {
	Verify(ctx context.Context, bearerToken string) (*Principal, error)
}

func ValidatePrincipal(principal *Principal) (*Principal, error) {
	if principal == nil {
		return nil, ErrNilPrincipal
	}
	if principal.Subject == "" {
		return nil, ErrEmptySubject
	}
	if principal.Email == "" {
		return nil, ErrEmptyEmail
	}
	return principal, nil
}
