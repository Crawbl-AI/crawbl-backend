package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/proto"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httputil"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/convert"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
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
			UserID: user.ID, WorkspaceID: chi.URLParam(r, "workspaceId"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		msgs := make([]proto.Message, 0, len(agents))
		for _, agent := range agents {
			msgs = append(msgs, convert.AgentToProto(agent))
		}
		WriteProtoArraySuccess(w, http.StatusOK, msgs)
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
			UserID: user.ID, WorkspaceID: chi.URLParam(r, "workspaceId"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		msgs := make([]proto.Message, 0, len(conversations))
		for _, conv := range conversations {
			msgs = append(msgs, convert.ConversationToProto(conv))
		}
		WriteProtoArraySuccess(w, http.StatusOK, msgs)
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
			UserID:         user.ID,
			WorkspaceID:    chi.URLParam(r, "workspaceId"),
			ConversationID: chi.URLParam(r, "id"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		WriteProtoSuccess(w, http.StatusOK, convert.ConversationToProto(conversation))
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

		messages := make([]*mobilev1.MessageResponse, 0, len(page.Data))
		for _, message := range page.Data {
			messages = append(messages, convert.MessageToProto(message))
		}

		WriteProtoSuccess(w, http.StatusOK, &mobilev1.MessagesListResponse{
			Messages: messages,
			Pagination: &mobilev1.MessagesPaginationResponse{
				NextScrollId: page.Pagination.NextScrollID,
				PrevScrollId: page.Pagination.PrevScrollID,
				HasNext:      page.Pagination.HasNext,
				HasPrev:      page.Pagination.HasPrev,
			},
		})
	}
}

// ConversationCreate creates a new conversation within a workspace.
// The request body must specify the conversation type ("swarm" or "agent").
// For agent conversations, agent_id must be provided.
// Returns 201 Created.
func ConversationCreate(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var reqBody mobilev1.CreateConversationRequest
		if err := DecodeProtoJSON(r, &reqBody); err != nil {
			WriteError(w, merrors.ErrInvalidInput)
			return
		}

		convType := orchestrator.ConversationType(strings.TrimSpace(reqBody.GetType()))
		if convType != orchestrator.ConversationTypeSwarm && convType != orchestrator.ConversationTypeAgent {
			WriteError(w, merrors.ErrInvalidInput)
			return
		}
		if convType == orchestrator.ConversationTypeAgent && strings.TrimSpace(reqBody.GetAgentId()) == "" {
			WriteError(w, merrors.ErrInvalidInput)
			return
		}

		conversation, mErr := c.ChatService.CreateConversation(r.Context(), &orchestratorservice.CreateConversationOpts{
			UserID:      user.ID,
			WorkspaceID: chi.URLParam(r, "workspaceId"),
			Type:        convType,
			AgentID:     strings.TrimSpace(reqBody.GetAgentId()),
			Title:       strings.TrimSpace(reqBody.GetTitle()),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteProtoSuccess(w, http.StatusCreated, convert.ConversationToProto(conversation))
	}
}

// ConversationDelete removes a conversation from a workspace.
// Returns 204 No Content on success.
func ConversationDelete(c *Context) http.HandlerFunc {
	return AuthedHandlerNoContent(c, func(r *http.Request, deps *AuthedHandlerDeps) *merrors.Error {
		return c.ChatService.DeleteConversation(r.Context(), &orchestratorservice.DeleteConversationOpts{
			UserID:         deps.User.ID,
			WorkspaceID:    chi.URLParam(r, "workspaceId"),
			ConversationID: chi.URLParam(r, "id"),
		})
	})
}

// SearchMessages searches conversation messages by text.
// GET /v1/workspaces/{workspaceId}/conversations/{id}/messages/search?q=...
// Not yet implemented — real full-text search comes later.
func SearchMessages(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httputil.WriteErrorMessage(w, http.StatusNotImplemented, "message search is not yet available")
	}
}

// ConversationMarkRead resets the unread count for a conversation to zero.
// The conversation must belong to a workspace owned by the authenticated user.
// Returns 204 No Content on success.
func ConversationMarkRead(c *Context) http.HandlerFunc {
	return AuthedHandlerNoContent(c, func(r *http.Request, deps *AuthedHandlerDeps) *merrors.Error {
		return c.ChatService.MarkConversationRead(r.Context(), &orchestratorservice.MarkConversationReadOpts{
			UserID:         deps.User.ID,
			WorkspaceID:    chi.URLParam(r, "workspaceId"),
			ConversationID: chi.URLParam(r, "id"),
		})
	})
}

// MessagesSend creates a new message in a conversation.
// The message is sent to the agent runtime for processing.
// Supports text content and file attachments via the request body.
//
// Returns 201 Created, needs OnPersisted callback, and logs error context —
// stays on the plain http.HandlerFunc form.
func MessagesSend(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var reqBody mobilev1.SendMessageRequest
		if err := DecodeProtoJSON(r, &reqBody); err != nil {
			httputil.WriteErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}

		content, mErr := convert.MessageContentToDomain(reqBody.GetContent())
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var userMsg *orchestrator.Message
		_, mErr = c.ChatService.SendMessage(r.Context(), &orchestratorservice.SendMessageOpts{
			UserID:         user.ID,
			WorkspaceID:    chi.URLParam(r, "workspaceId"),
			ConversationID: chi.URLParam(r, "id"),
			LocalID:        reqBody.GetLocalId(),
			Content:        content,
			Attachments:    convert.AttachmentsToDomain(reqBody.GetAttachments()),
			Mentions:       convert.MentionsToDomain(reqBody.GetMentions()),
			OnPersisted: func(msg *orchestrator.Message) {
				userMsg = msg
			},
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

		if userMsg == nil {
			c.Logger.Error("send message: OnPersisted not called, user message is nil",
				"path", r.URL.Path,
				"user_id", user.ID,
			)
			httputil.WriteErrorMessage(w, http.StatusInternalServerError, "internal error")
			return
		}
		WriteProtoSuccess(w, http.StatusCreated, convert.MessageToProto(userMsg))
	}
}
