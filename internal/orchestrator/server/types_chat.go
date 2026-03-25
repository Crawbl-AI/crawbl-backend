package server

import "time"

type conversationResponse struct {
	ID          string           `json:"id"`
	Type        string           `json:"type"`
	Title       string           `json:"title"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
	UnreadCount int              `json:"unreadCount"`
	Agent       *agentResponse   `json:"agent,omitempty"`
	LastMessage *messageResponse `json:"lastMessage,omitempty"`
}

type actionItemResponse struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Style string `json:"style"`
}

type attachmentResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	MIMEType string `json:"mimeType,omitempty"`
	Duration *int   `json:"duration,omitempty"`
}

type messageContentPayload struct {
	Type             string               `json:"type"`
	Text             string               `json:"text,omitempty"`
	Title            string               `json:"title,omitempty"`
	Description      string               `json:"description,omitempty"`
	Actions          []actionItemResponse `json:"actions,omitempty"`
	SelectedActionID *string              `json:"selectedActionId,omitempty"`
	Tool             string               `json:"tool,omitempty"`
	State            string               `json:"state,omitempty"`
}

type messageResponse struct {
	ID             string                `json:"id"`
	ConversationID string                `json:"conversationId"`
	Role           string                `json:"role"`
	Content        messageContentPayload `json:"content"`
	Status         string                `json:"status"`
	CreatedAt      time.Time             `json:"createdAt"`
	UpdatedAt      time.Time             `json:"updatedAt"`
	LocalID        *string               `json:"localId,omitempty"`
	Agent          *agentResponse        `json:"agent,omitempty"`
	Attachments    []attachmentResponse  `json:"attachments"`
}

type messagesPaginationResponse struct {
	NextScrollID string `json:"nextScrollId"`
	PrevScrollID string `json:"prevScrollId"`
	HasNext      bool   `json:"hasNext"`
	HasPrev      bool   `json:"hasPrev"`
}

type messagesListResponse struct {
	Data       []messageResponse          `json:"data"`
	Pagination messagesPaginationResponse `json:"pagination"`
}

type sendMessageRequest struct {
	LocalID     string                `json:"localId"`
	Content     messageContentPayload `json:"content"`
	Attachments []attachmentResponse  `json:"attachments"`
}
