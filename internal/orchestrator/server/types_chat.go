package server

import "time"

// conversationResponse represents a conversation in API responses.
// Conversations are threads of messages between a user and an agent.
type conversationResponse struct {
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
	Agent *agentResponse `json:"agent,omitempty"`

	// LastMessage is the most recent message in the conversation, for preview.
	LastMessage *messageResponse `json:"last_message,omitempty"`
}

// actionItemResponse represents a clickable action button in a message.
// Actions allow users to interact with agent responses beyond simple text.
type actionItemResponse struct {
	// ID is the unique identifier for this action.
	ID string `json:"id"`

	// Label is the display text shown to the user on the action button.
	Label string `json:"label"`

	// Style determines the visual presentation (e.g., "primary", "secondary", "danger").
	Style string `json:"style"`
}

// attachmentResponse represents a file attached to a message.
// Attachments can be images, documents, audio, or other file types.
type attachmentResponse struct {
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

// messageContentPayload represents the content of a message in requests and responses.
// Content can be text, structured data, or interactive elements with actions.
type messageContentPayload struct {
	// Type is the content type (e.g., "text", "action", "tool_result").
	Type string `json:"type"`

	// Text is the main text content for text-type messages.
	Text string `json:"text,omitempty"`

	// Title is an optional title for structured content.
	Title string `json:"title,omitempty"`

	// Description provides additional context for structured content.
	Description string `json:"description,omitempty"`

	// Actions contains interactive action buttons for user response.
	Actions []actionItemResponse `json:"actions,omitempty"`

	// SelectedActionID is the ID of the action selected by the user, if applicable.
	SelectedActionID *string `json:"selected_action_id,omitempty"`

	// Tool is the name of the tool associated with this content, for tool invocations.
	Tool string `json:"tool,omitempty"`

	// State is the current state of a tool invocation (e.g., "pending", "success", "error").
	State string `json:"state,omitempty"`
}

// messageResponse represents a message in API responses.
// Messages are the atomic units of conversation content.
type messageResponse struct {
	// ID is the unique identifier for the message.
	ID string `json:"id"`

	// ConversationID is the ID of the conversation containing this message.
	ConversationID string `json:"conversation_id"`

	// Role indicates who sent the message (e.g., "user", "assistant", "system").
	Role string `json:"role"`

	// Content contains the message body, which may include text, actions, or tool results.
	Content messageContentPayload `json:"content"`

	// Status is the message processing status (e.g., "sent", "delivered", "read").
	Status string `json:"status"`

	// CreatedAt is the timestamp when the message was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the timestamp when the message was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// LocalID is the client-provided ID for optimistic UI updates.
	LocalID *string `json:"local_id,omitempty"`

	// Agent is the agent that sent this message, for non-user messages.
	Agent *agentResponse `json:"agent,omitempty"`

	// Attachments contains any files attached to this message.
	Attachments []attachmentResponse `json:"attachments"`
}

// messagesPaginationResponse provides cursor-based pagination metadata for message lists.
// This allows efficient bidirectional scrolling through conversation history.
type messagesPaginationResponse struct {
	// NextScrollID is the cursor for fetching the next page of older messages.
	NextScrollID string `json:"next_scroll_id"`

	// PrevScrollID is the cursor for fetching the previous page of newer messages.
	PrevScrollID string `json:"prev_scroll_id"`

	// HasNext indicates whether more older messages exist.
	HasNext bool `json:"has_next"`

	// HasPrev indicates whether more newer messages exist.
	HasPrev bool `json:"has_prev"`
}

// messagesListResponse is the paginated response for listing messages.
// Includes both the message data and pagination cursors for scrolling.
type messagesListResponse struct {
	// Data contains the messages for the current page.
	Data []messageResponse `json:"data"`

	// Pagination contains the cursors and flags for scrolling through results.
	Pagination messagesPaginationResponse `json:"pagination"`
}

// sendMessageRequest represents the request body for sending a new message.
// Supports text content and file attachments.
type sendMessageRequest struct {
	// LocalID is a client-generated ID for optimistic updates and deduplication.
	LocalID string `json:"local_id"`

	// Content is the message body containing text and optional structured data.
	Content messageContentPayload `json:"content"`

	// Attachments contains files to include with the message.
	Attachments []attachmentResponse `json:"attachments"`
}
