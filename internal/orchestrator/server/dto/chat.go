package dto

import (
	"strings"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// ConversationResponse represents a conversation in API responses.
// Conversations are threads of messages between a user and an agent.
type ConversationResponse struct {
	// ID is the unique identifier for the conversation.
	ID string `json:"id"`

	// Type indicates the conversation type (e.g., "direct", "group").
	Type string `json:"type"`

	// Title is the display title of the conversation.
	Title string `json:"title"`

	// CreatedAt is the timestamp when the conversation was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the timestamp when the conversation was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// UnreadCount is the number of messages the user has not yet read.
	UnreadCount int `json:"unread_count"`

	// Agent is the agent participating in this conversation, if applicable.
	Agent *AgentResponse `json:"agent,omitempty"`

	// LastMessage is the most recent message in the conversation, for preview.
	LastMessage *MessageResponse `json:"last_message,omitempty"`
}

// ActionItemResponse represents a clickable action button in a message.
// Actions allow users to interact with agent responses beyond simple text.
type ActionItemResponse struct {
	// ID is the unique identifier for this action.
	ID string `json:"id"`

	// Label is the display text shown to the user on the action button.
	Label string `json:"label"`

	// Style determines the visual presentation (e.g., "primary", "secondary", "danger").
	Style string `json:"style"`
}

// AttachmentResponse represents a file attached to a message.
// Attachments can be images, documents, audio, or other file types.
type AttachmentResponse struct {
	// ID is the unique identifier for the attachment.
	ID string `json:"id"`

	// Name is the original filename of the attachment.
	Name string `json:"name"`

	// URL is the download URL for the attachment content.
	URL string `json:"url"`

	// Type is the attachment type category (e.g., "image", "document", "audio").
	Type string `json:"type"`

	// Size is the file size in bytes.
	Size int64 `json:"size"`

	// MIMEType is the MIME type of the file content (e.g., "image/png").
	MIMEType string `json:"mime_type,omitempty"`

	// Duration is the duration in seconds for audio/video attachments.
	Duration *int `json:"duration,omitempty"`
}

// MessageContentPayload represents the content of a message in requests and responses.
// Content can be text, structured data, or interactive elements with actions.
type MessageContentPayload struct {
	// Type is the content type (e.g., "text", "action", "tool_result").
	Type string `json:"type"`

	// Text is the main text content for text-type messages.
	Text string `json:"text,omitempty"`

	// Title is an optional title for structured content.
	Title string `json:"title,omitempty"`

	// Description provides additional context for structured content.
	Description string `json:"description,omitempty"`

	// Actions contains interactive action buttons for user response.
	Actions []ActionItemResponse `json:"actions,omitempty"`

	// SelectedActionID is the ID of the action selected by the user, if applicable.
	SelectedActionID *string `json:"selected_action_id,omitempty"`

	// Tool status fields (type = "tool_status").
	Tool  string         `json:"tool,omitempty"`
	State string         `json:"state,omitempty"`
	Query string         `json:"query,omitempty"`
	Args  map[string]any `json:"args,omitempty"`

	// Delegation fields (type = "delegation").
	From             *orchestrator.ContentAgent `json:"from,omitempty"`
	To               *orchestrator.ContentAgent `json:"to,omitempty"`
	DelegationStatus string                     `json:"status,omitempty"`
	TaskPreview      string                     `json:"task_preview,omitempty"`
}

// MessageResponse represents a message in API responses.
// Messages are the atomic units of conversation content.
type MessageResponse struct {
	// ID is the unique identifier for the message.
	ID string `json:"id"`

	// ConversationID is the ID of the conversation containing this message.
	ConversationID string `json:"conversation_id"`

	// Role indicates who sent the message (e.g., "user", "assistant", "system").
	Role string `json:"role"`

	// Content contains the message body, which may include text, actions, or tool results.
	Content MessageContentPayload `json:"content"`

	// Status is the message processing status (e.g., "sent", "delivered", "read").
	Status string `json:"status"`

	// CreatedAt is the timestamp when the message was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the timestamp when the message was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// LocalID is the client-provided ID for optimistic UI updates.
	LocalID *string `json:"local_id,omitempty"`

	// Agent is the agent that sent this message, for non-user messages.
	Agent *AgentResponse `json:"agent,omitempty"`

	// Attachments contains any files attached to this message.
	Attachments []AttachmentResponse `json:"attachments"`
}

// MessagesPaginationResponse provides cursor-based pagination metadata for message lists.
// This allows efficient bidirectional scrolling through conversation history.
type MessagesPaginationResponse struct {
	// NextScrollID is the cursor for fetching the next page of older messages.
	NextScrollID string `json:"next_scroll_id"`

	// PrevScrollID is the cursor for fetching the previous page of newer messages.
	PrevScrollID string `json:"prev_scroll_id"`

	// HasNext indicates whether more older messages exist.
	HasNext bool `json:"has_next"`

	// HasPrev indicates whether more newer messages exist.
	HasPrev bool `json:"has_prev"`
}

// MessagesListResponse is the paginated response for listing messages.
// Emitted through the WriteSuccess envelope helper, so the final wire
// shape is {"data": {"messages": [...], "pagination": {...}}} — the
// outer "data" is the envelope, the inner "messages" is this struct's
// Messages field. Do NOT rename the field to Data; that produces a
// {"data": {"data": [...]}} double-envelope the mobile client cannot
// parse.
type MessagesListResponse struct {
	// Messages is the list of messages for the current page.
	Messages []MessageResponse `json:"messages"`

	// Pagination contains the cursors and flags for scrolling through results.
	Pagination MessagesPaginationResponse `json:"pagination"`
}

// CreateConversationRequest represents the request body for creating a new conversation.
type CreateConversationRequest struct {
	// Type is the conversation type: "swarm" or "agent".
	Type string `json:"type"`
	// AgentID is the agent to associate with the conversation.
	// Required when Type is "agent"; omitted for swarm conversations.
	AgentID string `json:"agent_id,omitempty"`
}

// SendMessageRequest represents the request body for sending a new message.
// Supports text content and file attachments.
type SendMessageRequest struct {
	// LocalID is a client-generated ID for optimistic updates and deduplication.
	LocalID string `json:"local_id"`

	// Content is the message body containing text and optional structured data.
	Content MessageContentPayload `json:"content"`

	// Attachments contains files to include with the message.
	Attachments []AttachmentResponse `json:"attachments"`

	// Mentions is the list of @-mentioned agents in the message (swarm chat).
	Mentions []MentionPayload `json:"mentions"`
}

// MentionPayload represents an @-mention of an agent in a message.
type MentionPayload struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Offset    int    `json:"offset"`
	Length    int    `json:"length"`
}

// ActionCardRequest represents the request body for responding to an action card message.
type ActionCardRequest struct {
	// ActionID is the ID of the action selected by the user.
	ActionID string `json:"action_id"`
}

// ToConversationResponse converts a domain Conversation to the API response format.
// Includes nested agent and last message information for conversation previews.
func ToConversationResponse(conversation *orchestrator.Conversation) ConversationResponse {
	response := ConversationResponse{
		ID:          conversation.ID,
		Type:        string(conversation.Type),
		Title:       conversation.Title,
		CreatedAt:   conversation.CreatedAt,
		UpdatedAt:   conversation.UpdatedAt,
		UnreadCount: conversation.UnreadCount,
	}
	if conversation.Agent != nil {
		agent := ToAgentResponse(conversation.Agent)
		response.Agent = &agent
	}
	if conversation.LastMessage != nil {
		message := ToMessageResponse(conversation.LastMessage)
		response.LastMessage = &message
	}
	return response
}

// ToMessageResponse converts a domain Message to the API response format.
// Includes the associated agent if present, and transforms content and attachments.
func ToMessageResponse(message *orchestrator.Message) MessageResponse {
	response := MessageResponse{
		ID:             message.ID,
		ConversationID: message.ConversationID,
		Role:           string(message.Role),
		Content:        MessageContentFromDomain(message.Content),
		Status:         string(message.Status),
		CreatedAt:      message.CreatedAt,
		UpdatedAt:      message.UpdatedAt,
		LocalID:        message.LocalID,
		Attachments:    AttachmentsFromDomain(message.Attachments),
	}
	if message.Agent != nil {
		agent := ToAgentResponse(message.Agent)
		response.Agent = &agent
	}
	return response
}

// MessageContentFromDomain converts domain MessageContent to the API response format.
// Handles the transformation of content types, actions, and tool state.
func MessageContentFromDomain(content orchestrator.MessageContent) MessageContentPayload {
	response := MessageContentPayload{
		Type:             string(content.Type),
		Text:             content.Text,
		Title:            content.Title,
		Description:      content.Description,
		Tool:             content.Tool,
		State:            string(content.State),
		Query:            content.Query,
		Args:             content.Args,
		From:             content.From,
		To:               content.To,
		DelegationStatus: content.Status,
		TaskPreview:      content.TaskPreview,
	}
	if content.SelectedActionID != nil {
		response.SelectedActionID = content.SelectedActionID
	}
	if len(content.Actions) > 0 {
		response.Actions = make([]ActionItemResponse, 0, len(content.Actions))
		for _, action := range content.Actions {
			response.Actions = append(response.Actions, ActionItemResponse{
				ID:    action.ID,
				Label: action.Label,
				Style: string(action.Style),
			})
		}
	}
	return response
}

// ToDomain converts the API message content payload to domain MessageContent.
// Validates that the content type is supported (currently only text is allowed)
// and that text content is present for text messages.
func (payload MessageContentPayload) ToDomain() (orchestrator.MessageContent, *merrors.Error) {
	contentType := orchestrator.MessageContentType(strings.TrimSpace(payload.Type))
	if contentType == "" {
		contentType = orchestrator.MessageContentTypeText
	}

	content := orchestrator.MessageContent{
		Type:             contentType,
		Text:             payload.Text,
		Title:            payload.Title,
		Description:      payload.Description,
		SelectedActionID: payload.SelectedActionID,
		Tool:             payload.Tool,
		State:            orchestrator.ToolState(payload.State),
	}
	if len(payload.Actions) > 0 {
		content.Actions = make([]orchestrator.ActionItem, 0, len(payload.Actions))
		for _, action := range payload.Actions {
			content.Actions = append(content.Actions, orchestrator.ActionItem{
				ID:    action.ID,
				Label: action.Label,
				Style: orchestrator.ActionStyle(action.Style),
			})
		}
	}

	if content.Type != orchestrator.MessageContentTypeText {
		return orchestrator.MessageContent{}, merrors.ErrUnsupportedMessage
	}
	if strings.TrimSpace(content.Text) == "" {
		return orchestrator.MessageContent{}, merrors.ErrEmptyMessageText
	}

	return content, nil
}

// AttachmentsFromDomain converts domain Attachments to the API response format.
// Returns an empty slice if no attachments are present.
func AttachmentsFromDomain(attachments []orchestrator.Attachment) []AttachmentResponse {
	if len(attachments) == 0 {
		return []AttachmentResponse{}
	}

	response := make([]AttachmentResponse, 0, len(attachments))
	for _, attachment := range attachments {
		response = append(response, AttachmentResponse{
			ID:       attachment.ID,
			Name:     attachment.Name,
			URL:      attachment.URL,
			Type:     string(attachment.Type),
			Size:     attachment.Size,
			MIMEType: attachment.MIMEType,
			Duration: attachment.Duration,
		})
	}
	return response
}

// AttachmentsToDomain converts API attachment responses to domain Attachments.
// Returns an empty slice if no attachments are provided.
func AttachmentsToDomain(attachments []AttachmentResponse) []orchestrator.Attachment {
	if len(attachments) == 0 {
		return []orchestrator.Attachment{}
	}

	response := make([]orchestrator.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		response = append(response, orchestrator.Attachment{
			ID:       attachment.ID,
			Name:     attachment.Name,
			URL:      attachment.URL,
			Type:     orchestrator.AttachmentType(attachment.Type),
			Size:     attachment.Size,
			MIMEType: attachment.MIMEType,
			Duration: attachment.Duration,
		})
	}
	return response
}

// MentionsToDomain converts API mention payloads to domain Mentions.
func MentionsToDomain(mentions []MentionPayload) []orchestrator.Mention {
	if len(mentions) == 0 {
		return nil
	}
	result := make([]orchestrator.Mention, 0, len(mentions))
	for _, m := range mentions {
		result = append(result, orchestrator.Mention{
			AgentID:   m.AgentID,
			AgentName: m.AgentName,
			Offset:    m.Offset,
			Length:    m.Length,
		})
	}
	return result
}
