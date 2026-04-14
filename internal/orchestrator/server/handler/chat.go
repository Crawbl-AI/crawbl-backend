package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// workspaceListHandler builds a handler that lists workspace-scoped items
// (agents, conversations, etc.), maps each domain entity to its response
// DTO, and returns the flat slice wrapped in the standard envelope.
// Collapses the shared WorkspaceAgentsList / ConversationsList scaffolding.
func workspaceListHandler[Domain any, Response any](
	c *Context,
	list func(ctx context.Context, userID, workspaceID string) ([]Domain, *merrors.Error),
	toResponse func(Domain) Response,
) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) ([]Response, *merrors.Error) {
		items, mErr := list(r.Context(), deps.User.ID, chi.URLParam(r, "workspaceId"))
		if mErr != nil {
			return nil, mErr
		}
		response := make([]Response, 0, len(items))
		for _, item := range items {
			response = append(response, toResponse(item))
		}
		return response, nil
	})
}

// WorkspaceAgentsList retrieves all agents available in a workspace.
// Agents represent individual swarm members that users can interact with
// through conversations.
func WorkspaceAgentsList(c *Context) http.HandlerFunc {
	return workspaceListHandler(c,
		func(ctx context.Context, userID, workspaceID string) ([]*orchestrator.Agent, *merrors.Error) {
			return c.ChatService.ListAgents(ctx, &orchestratorservice.ListAgentsOpts{
				UserID: userID, WorkspaceID: workspaceID,
			})
		},
		dto.ToAgentResponse,
	)
}

// ConversationsList retrieves all conversations for a workspace.
// Each conversation includes its associated agent and last message for preview.
func ConversationsList(c *Context) http.HandlerFunc {
	return workspaceListHandler(c,
		func(ctx context.Context, userID, workspaceID string) ([]*orchestrator.Conversation, *merrors.Error) {
			return c.ChatService.ListConversations(ctx, &orchestratorservice.ListConversationsOpts{
				UserID: userID, WorkspaceID: workspaceID,
			})
		},
		dto.ToConversationResponse,
	)
}

// ConversationGet retrieves a single conversation by ID within a workspace.
// The conversation must belong to a workspace owned by the authenticated user.
func ConversationGet(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) (dto.ConversationResponse, *merrors.Error) {
		conversation, mErr := c.ChatService.GetConversation(r.Context(), &orchestratorservice.GetConversationOpts{
			UserID:         deps.User.ID,
			WorkspaceID:    chi.URLParam(r, "workspaceId"),
			ConversationID: chi.URLParam(r, "id"),
		})
		if mErr != nil {
			return dto.ConversationResponse{}, mErr
		}
		return dto.ToConversationResponse(conversation), nil
	})
}

// MessagesList retrieves messages for a conversation with cursor-based pagination.
// Supports bidirectional scrolling via scrollId and direction parameters.
// The limit parameter controls the page size for responses.
func MessagesList(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) (dto.MessagesListResponse, *merrors.Error) {
		page, mErr := c.ChatService.ListMessages(r.Context(), &orchestratorservice.ListMessagesOpts{
			UserID:         deps.User.ID,
			WorkspaceID:    chi.URLParam(r, "workspaceId"),
			ConversationID: chi.URLParam(r, "id"),
			ScrollID:       strings.TrimSpace(r.URL.Query().Get("scrollId")),
			Limit:          IntQueryParam(r, "limit"),
			Direction:      strings.TrimSpace(r.URL.Query().Get("direction")),
		})
		if mErr != nil {
			return dto.MessagesListResponse{}, mErr
		}

		response := make([]dto.MessageResponse, 0, len(page.Data))
		for _, message := range page.Data {
			response = append(response, dto.ToMessageResponse(message))
		}

		return dto.MessagesListResponse{
			Messages: response,
			Pagination: dto.MessagesPaginationResponse{
				NextScrollID: page.Pagination.NextScrollID,
				PrevScrollID: page.Pagination.PrevScrollID,
				HasNext:      page.Pagination.HasNext,
				HasPrev:      page.Pagination.HasPrev,
			},
		}, nil
	})
}

// ConversationCreate creates a new conversation within a workspace.
// The request body must specify the conversation type ("swarm" or "agent").
// For agent conversations, agent_id must be provided.
// Returns 201 Created.
func ConversationCreate(c *Context) http.HandlerFunc {
	return AuthedHandlerCreated(c, func(r *http.Request, deps *AuthedHandlerDeps, reqBody *dto.CreateConversationRequest) (dto.ConversationResponse, *merrors.Error) {
		convType := orchestrator.ConversationType(strings.TrimSpace(reqBody.Type))
		if convType != orchestrator.ConversationTypeSwarm && convType != orchestrator.ConversationTypeAgent {
			return dto.ConversationResponse{}, merrors.ErrInvalidInput
		}
		if convType == orchestrator.ConversationTypeAgent && strings.TrimSpace(reqBody.AgentID) == "" {
			return dto.ConversationResponse{}, merrors.ErrInvalidInput
		}

		conversation, mErr := c.ChatService.CreateConversation(r.Context(), &orchestratorservice.CreateConversationOpts{
			UserID:      deps.User.ID,
			WorkspaceID: chi.URLParam(r, "workspaceId"),
			Type:        convType,
			AgentID:     strings.TrimSpace(reqBody.AgentID),
		})
		if mErr != nil {
			return dto.ConversationResponse{}, mErr
		}

		return dto.ToConversationResponse(conversation), nil
	})
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
		httpserver.WriteErrorMessage(w, http.StatusNotImplemented, "message search is not yet available")
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

		var reqBody dto.SendMessageRequest
		if err := DecodeJSON(r, &reqBody); err != nil {
			httpserver.WriteErrorMessage(w, http.StatusBadRequest, "invalid request body")
			return
		}

		content, mErr := reqBody.Content.ToDomain()
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var userMsg *orchestrator.Message
		_, mErr = c.ChatService.SendMessage(r.Context(), &orchestratorservice.SendMessageOpts{
			UserID:         user.ID,
			WorkspaceID:    chi.URLParam(r, "workspaceId"),
			ConversationID: chi.URLParam(r, "id"),
			LocalID:        reqBody.LocalID,
			Content:        content,
			Attachments:    dto.AttachmentsToDomain(reqBody.Attachments),
			Mentions:       dto.MentionsToDomain(reqBody.Mentions),
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
			httpserver.WriteErrorMessage(w, http.StatusInternalServerError, "internal error")
			return
		}
		WriteSuccess(w, http.StatusCreated, dto.ToMessageResponse(userMsg))
	}
}
