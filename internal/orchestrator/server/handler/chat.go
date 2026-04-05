package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// WorkspaceAgentsList retrieves all agents available in a workspace.
// Agents represent individual swarm members that users can interact with
// through conversations.
func WorkspaceAgentsList(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		agents, mErr := c.ChatService.ListAgents(r.Context(), &orchestratorservice.ListAgentsOpts{
			Sess:        c.NewSession(),
			UserID:      user.ID,
			WorkspaceID: chi.URLParam(r, "workspaceId"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		response := make([]dto.AgentResponse, 0, len(agents))
		for _, agent := range agents {
			response = append(response, dto.ToAgentResponse(agent))
		}

		WriteSuccess(w, http.StatusOK, response)
	}
}

// ConversationsList retrieves all conversations for a workspace.
// Each conversation includes its associated agent and last message for preview.
func ConversationsList(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		conversations, mErr := c.ChatService.ListConversations(r.Context(), &orchestratorservice.ListConversationsOpts{
			Sess:        c.NewSession(),
			UserID:      user.ID,
			WorkspaceID: chi.URLParam(r, "workspaceId"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		response := make([]dto.ConversationResponse, 0, len(conversations))
		for _, conversation := range conversations {
			response = append(response, dto.ToConversationResponse(conversation))
		}

		WriteSuccess(w, http.StatusOK, response)
	}
}

// ConversationGet retrieves a single conversation by ID within a workspace.
// The conversation must belong to a workspace owned by the authenticated user.
func ConversationGet(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		conversation, mErr := c.ChatService.GetConversation(r.Context(), &orchestratorservice.GetConversationOpts{
			Sess:           c.NewSession(),
			UserID:         user.ID,
			WorkspaceID:    chi.URLParam(r, "workspaceId"),
			ConversationID: chi.URLParam(r, "id"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, dto.ToConversationResponse(conversation))
	}
}

// MessagesList retrieves messages for a conversation with cursor-based pagination.
// Supports bidirectional scrolling via scrollId and direction parameters.
// The limit parameter controls the page size for responses.
func MessagesList(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		page, mErr := c.ChatService.ListMessages(r.Context(), &orchestratorservice.ListMessagesOpts{
			Sess:           c.NewSession(),
			UserID:         user.ID,
			WorkspaceID:    chi.URLParam(r, "workspaceId"),
			ConversationID: chi.URLParam(r, "id"),
			ScrollID:       strings.TrimSpace(r.URL.Query().Get("scrollId")),
			Limit:          IntQueryParam(r, "limit"),
			Direction:      strings.TrimSpace(r.URL.Query().Get("direction")),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		response := make([]dto.MessageResponse, 0, len(page.Data))
		for _, message := range page.Data {
			response = append(response, dto.ToMessageResponse(message))
		}

		WriteJSON(w, http.StatusOK, &dto.MessagesListResponse{
			Data: response,
			Pagination: dto.MessagesPaginationResponse{
				NextScrollID: page.Pagination.NextScrollID,
				PrevScrollID: page.Pagination.PrevScrollID,
				HasNext:      page.Pagination.HasNext,
				HasPrev:      page.Pagination.HasPrev,
			},
		})
	}
}

// MessagesSend creates a new message in a conversation.
// The message is sent to the agent runtime for processing.
// Supports text content and file attachments via the request body.
func MessagesSend(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var reqBody dto.SendMessageRequest
		if err := DecodeJSON(r, &reqBody); err != nil {
			httpserver.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
			return
		}

		content, mErr := reqBody.Content.ToDomain()
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		replyMsgs, mErr := c.ChatService.SendMessage(r.Context(), &orchestratorservice.SendMessageOpts{
			Sess:           c.NewSession(),
			UserID:         user.ID,
			WorkspaceID:    chi.URLParam(r, "workspaceId"),
			ConversationID: chi.URLParam(r, "id"),
			LocalID:        reqBody.LocalID,
			Content:        content,
			Attachments:    dto.AttachmentsToDomain(reqBody.Attachments),
			Mentions:       dto.MentionsToDomain(reqBody.Mentions),
		})
		if mErr != nil {
			c.Logger.Error("send message failed",
				"path", r.URL.Path,
				"user_id", user.ID,
				"error", mErr.Error(),
			)
			WriteError(w, mErr)
			return
		}

		response := make([]dto.MessageResponse, 0, len(replyMsgs))
		for _, msg := range replyMsgs {
			response = append(response, dto.ToMessageResponse(msg))
		}
		WriteSuccess(w, http.StatusOK, response)
	}
}
