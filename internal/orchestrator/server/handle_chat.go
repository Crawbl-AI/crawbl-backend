package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// handleWorkspaceAgentsList retrieves all agents available in a workspace.
// Agents represent individual swarm members that users can interact with
// through conversations.
func (s *Server) handleWorkspaceAgentsList(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	agents, mErr := s.chatService.ListAgents(r.Context(), &orchestratorservice.ListAgentsOpts{
		Sess:        s.newSession(),
		UserID:      user.ID,
		WorkspaceID: chi.URLParam(r, "workspaceId"),
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	response := make([]agentResponse, 0, len(agents))
	for _, agent := range agents {
		response = append(response, toAgentResponse(agent))
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, response)
}

// handleConversationsList retrieves all conversations for a workspace.
// Each conversation includes its associated agent and last message for preview.
func (s *Server) handleConversationsList(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	conversations, mErr := s.chatService.ListConversations(r.Context(), &orchestratorservice.ListConversationsOpts{
		Sess:        s.newSession(),
		UserID:      user.ID,
		WorkspaceID: chi.URLParam(r, "workspaceId"),
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	response := make([]conversationResponse, 0, len(conversations))
	for _, conversation := range conversations {
		response = append(response, toConversationResponse(conversation))
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, response)
}

// handleConversationGet retrieves a single conversation by ID within a workspace.
// The conversation must belong to a workspace owned by the authenticated user.
func (s *Server) handleConversationGet(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	conversation, mErr := s.chatService.GetConversation(r.Context(), &orchestratorservice.GetConversationOpts{
		Sess:           s.newSession(),
		UserID:         user.ID,
		WorkspaceID:    chi.URLParam(r, "workspaceId"),
		ConversationID: chi.URLParam(r, "id"),
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, toConversationResponse(conversation))
}

// handleMessagesList retrieves messages for a conversation with cursor-based pagination.
// Supports bidirectional scrolling via scrollId and direction parameters.
// The limit parameter controls the page size for responses.
func (s *Server) handleMessagesList(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	page, mErr := s.chatService.ListMessages(r.Context(), &orchestratorservice.ListMessagesOpts{
		Sess:           s.newSession(),
		UserID:         user.ID,
		WorkspaceID:    chi.URLParam(r, "workspaceId"),
		ConversationID: chi.URLParam(r, "id"),
		ScrollID:       strings.TrimSpace(r.URL.Query().Get("scrollId")),
		Limit:          intQueryParam(r, "limit"),
		Direction:      strings.TrimSpace(r.URL.Query().Get("direction")),
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	response := make([]messageResponse, 0, len(page.Data))
	for _, message := range page.Data {
		response = append(response, toMessageResponse(message))
	}

	httpserver.WriteJSONResponse(w, http.StatusOK, &messagesListResponse{
		Data: response,
		Pagination: messagesPaginationResponse{
			NextScrollID: page.Pagination.NextScrollID,
			PrevScrollID: page.Pagination.PrevScrollID,
			HasNext:      page.Pagination.HasNext,
			HasPrev:      page.Pagination.HasPrev,
		},
	})
}

// handleMessagesSend creates a new message in a conversation.
// The message is sent to the ZeroClaw swarm for processing.
// Supports text content and file attachments via the request body.
func (s *Server) handleMessagesSend(w http.ResponseWriter, r *http.Request) {
	user, mErr := s.currentUserFromRequest(r)
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	var reqBody sendMessageRequest
	if err := decodeJSON(r, &reqBody); err != nil {
		httpserver.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	content, mErr := reqBody.Content.toDomain()
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	message, mErr := s.chatService.SendMessage(r.Context(), &orchestratorservice.SendMessageOpts{
		Sess:           s.newSession(),
		UserID:         user.ID,
		WorkspaceID:    chi.URLParam(r, "workspaceId"),
		ConversationID: chi.URLParam(r, "id"),
		LocalID:        reqBody.LocalID,
		Content:        content,
		Attachments:    attachmentsToDomain(reqBody.Attachments),
	})
	if mErr != nil {
		httpserver.WriteErrorResponse(w, httpStatusForError(mErr), merrors.PublicMessage(mErr))
		return
	}

	httpserver.WriteSuccessResponse(w, http.StatusOK, toMessageResponse(message))
}

// toAgentResponse converts a domain Agent to the API response format.
// Returns an empty response if the agent pointer is nil.
func toAgentResponse(agent *orchestrator.Agent) agentResponse {
	if agent == nil {
		return agentResponse{}
	}

	return agentResponse{
		ID:        agent.ID,
		Name:      agent.Name,
		Role:      agent.Role,
		Avatar:    agent.AvatarURL,
		Status:    string(agent.Status),
		HasUpdate: agent.HasUpdate,
	}
}

// toConversationResponse converts a domain Conversation to the API response format.
// Includes nested agent and last message information for conversation previews.
func toConversationResponse(conversation *orchestrator.Conversation) conversationResponse {
	response := conversationResponse{
		ID:          conversation.ID,
		Type:        string(conversation.Type),
		Title:       conversation.Title,
		CreatedAt:   conversation.CreatedAt,
		UpdatedAt:   conversation.UpdatedAt,
		UnreadCount: conversation.UnreadCount,
	}
	if conversation.Agent != nil {
		agent := toAgentResponse(conversation.Agent)
		response.Agent = &agent
	}
	if conversation.LastMessage != nil {
		message := toMessageResponse(conversation.LastMessage)
		response.LastMessage = &message
	}
	return response
}

// toMessageResponse converts a domain Message to the API response format.
// Includes the associated agent if present, and transforms content and attachments.
func toMessageResponse(message *orchestrator.Message) messageResponse {
	response := messageResponse{
		ID:             message.ID,
		ConversationID: message.ConversationID,
		Role:           string(message.Role),
		Content:        messageContentFromDomain(message.Content),
		Status:         string(message.Status),
		CreatedAt:      message.CreatedAt,
		UpdatedAt:      message.UpdatedAt,
		LocalID:        message.LocalID,
		Attachments:    attachmentsFromDomain(message.Attachments),
	}
	if message.Agent != nil {
		agent := toAgentResponse(message.Agent)
		response.Agent = &agent
	}
	return response
}

// messageContentFromDomain converts domain MessageContent to the API response format.
// Handles the transformation of content types, actions, and tool state.
func messageContentFromDomain(content orchestrator.MessageContent) messageContentPayload {
	response := messageContentPayload{
		Type:        string(content.Type),
		Text:        content.Text,
		Title:       content.Title,
		Description: content.Description,
		Tool:        content.Tool,
		State:       string(content.State),
	}
	if content.SelectedActionID != nil {
		response.SelectedActionID = content.SelectedActionID
	}
	if len(content.Actions) > 0 {
		response.Actions = make([]actionItemResponse, 0, len(content.Actions))
		for _, action := range content.Actions {
			response.Actions = append(response.Actions, actionItemResponse{
				ID:    action.ID,
				Label: action.Label,
				Style: string(action.Style),
			})
		}
	}
	return response
}

// toDomain converts the API message content payload to domain MessageContent.
// Validates that the content type is supported (currently only text is allowed)
// and that text content is present for text messages.
func (payload messageContentPayload) toDomain() (orchestrator.MessageContent, *merrors.Error) {
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
		return orchestrator.MessageContent{}, merrors.ErrUnsupportedMessage
	}

	return content, nil
}

// attachmentsFromDomain converts domain Attachments to the API response format.
// Returns an empty slice if no attachments are present.
func attachmentsFromDomain(attachments []orchestrator.Attachment) []attachmentResponse {
	if len(attachments) == 0 {
		return []attachmentResponse{}
	}

	response := make([]attachmentResponse, 0, len(attachments))
	for _, attachment := range attachments {
		response = append(response, attachmentResponse{
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

// attachmentsToDomain converts API attachment responses to domain Attachments.
// Returns an empty slice if no attachments are provided.
func attachmentsToDomain(attachments []attachmentResponse) []orchestrator.Attachment {
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

// intQueryParam extracts an integer value from a query parameter.
// Returns 0 if the parameter is missing or cannot be parsed as an integer.
func intQueryParam(r *http.Request, key string) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0
	}

	var parsed int
	_, _ = fmt.Sscanf(raw, "%d", &parsed)
	return parsed
}
