package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/convert"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// Agent memory field length limits — mirror the MCP path enforcement.
const (
	MaxAgentMemoryKeyLength      = 256
	MaxAgentMemoryContentLength  = 16 * 1024 // 16 KiB
	MaxAgentMemoryCategoryLength = 128
)

// GetAgent retrieves a single agent by ID.
// The agent must belong to a workspace owned by the authenticated user.
func GetAgent(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		agent, mErr := c.AgentService.GetAgent(r.Context(), &orchestratorservice.GetAgentOpts{
			UserID: user.ID, AgentID: chi.URLParam(r, "id"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		WriteProtoSuccess(w, http.StatusOK, convert.AgentToProto(agent))
	}
}

// GetAgentDetails retrieves full agent profile including stats.
// The agent must belong to a workspace owned by the authenticated user.
func GetAgentDetails(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		details, mErr := c.AgentService.GetAgentDetails(r.Context(), &orchestratorservice.GetAgentDetailsOpts{
			UserID: user.ID, AgentID: chi.URLParam(r, "id"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		WriteProtoSuccess(w, http.StatusOK, convert.AgentDetailToProto(details))
	}
}

// GetAgentHistory retrieves paginated conversation history for an agent.
func GetAgentHistory(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		limit, offset := Pagination(r)

		items, pagination, mErr := c.AgentService.GetAgentHistory(r.Context(), &orchestratorservice.GetAgentHistoryOpts{
			UserID:  user.ID,
			AgentID: chi.URLParam(r, "id"),
			Limit:   limit,
			Offset:  offset,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		historyItems := make([]*mobilev1.AgentHistoryItemResponse, 0, len(items))
		for _, item := range items {
			h := &mobilev1.AgentHistoryItemResponse{
				ConversationId: item.ConversationID,
				Title:          item.Title,
				Subtitle:       item.Subtitle,
			}
			if item.CreatedAt != nil {
				h.CreatedAt = timestamppb.New(*item.CreatedAt)
			}
			historyItems = append(historyItems, h)
		}

		WriteProtoSuccess(w, http.StatusOK, &mobilev1.AgentHistoryResponse{
			Items:      historyItems,
			Pagination: convert.OffsetPaginationToProto(*pagination),
		})
	}
}

// GetAgentSettings retrieves model and prompt settings for an agent.
func GetAgentSettings(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		settings, mErr := c.AgentService.GetAgentSettings(r.Context(), &orchestratorservice.GetAgentSettingsOpts{
			UserID:  user.ID,
			AgentID: chi.URLParam(r, "id"),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		prompts := make([]*mobilev1.AgentPromptResponse, 0, len(settings.Prompts))
		for _, p := range settings.Prompts {
			prompts = append(prompts, &mobilev1.AgentPromptResponse{
				Id:          p.ID,
				Name:        p.Name,
				Description: p.Description,
				Content:     p.Content,
			})
		}

		WriteProtoSuccess(w, http.StatusOK, &mobilev1.AgentSettingsResponse{
			Model:          settings.Model.ID,
			ResponseLength: string(settings.ResponseLength),
			Prompts:        prompts,
		})
	}
}

// GetAgentTools retrieves the tools assigned to an agent with offset pagination.
func GetAgentTools(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		limit, offset := Pagination(r)

		page, mErr := c.AgentService.GetAgentTools(r.Context(), &orchestratorservice.GetAgentToolsOpts{
			UserID:  user.ID,
			AgentID: chi.URLParam(r, "id"),
			Limit:   limit,
			Offset:  offset,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		tools := make([]*mobilev1.AgentToolResponse, 0, len(page.Data))
		for _, t := range page.Data {
			tools = append(tools, convert.AgentToolToProto(t))
		}

		WriteProtoSuccess(w, http.StatusOK, &mobilev1.AgentToolsResponse{
			Tools:      tools,
			Pagination: convert.OffsetPaginationToProto(page.Pagination),
		})
	}
}

// ListModels returns the list of available LLM models.
// This is a public endpoint — no auth required (loaded by DictService before login).
func ListModels(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		available := orchestrator.GetAvailableModels()
		msgs := make([]proto.Message, 0, len(available))
		for _, m := range available {
			msgs = append(msgs, &mobilev1.AgentModelResponse{
				Id:          m.ID,
				Name:        m.Name,
				Description: m.Description,
			})
		}
		WriteProtoArraySuccess(w, http.StatusOK, msgs)
	}
}

// GetAgentMemories retrieves memories from the agent's memory palace.
// Pagination is handled by the service/repo layer; the response is a flat
// array of memory entries inside the standard {"data": [...]} envelope.
func GetAgentMemories(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}
		category := r.URL.Query().Get("category")
		limit, offset := Pagination(r)

		memories, mErr := c.AgentService.GetAgentMemories(r.Context(), &orchestratorservice.GetAgentMemoriesOpts{
			UserID:   user.ID,
			AgentID:  chi.URLParam(r, "id"),
			Category: category,
			Limit:    limit,
			Offset:   offset,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		msgs := make([]proto.Message, 0, len(memories))
		for _, m := range memories {
			msgs = append(msgs, &mobilev1.AgentMemoryResponse{
				Key:       m.Key,
				Content:   m.Content,
				Category:  m.Category,
				CreatedAt: m.CreatedAt,
				UpdatedAt: m.UpdatedAt,
			})
		}
		WriteProtoArraySuccess(w, http.StatusOK, msgs)
	}
}

// DeleteAgentMemory removes a specific memory from the agent's agent runtime.
func DeleteAgentMemory(c *Context) http.HandlerFunc {
	return AuthedHandlerNoContent(c, func(r *http.Request, deps *AuthedHandlerDeps) *merrors.Error {
		return c.AgentService.DeleteAgentMemory(r.Context(), &orchestratorservice.DeleteAgentMemoryOpts{
			UserID:  deps.User.ID,
			AgentID: chi.URLParam(r, "id"),
			Key:     chi.URLParam(r, "key"),
		})
	})
}

// CreateAgentMemory stores a new memory in the agent's memory palace and
// returns the created entry (including server-generated timestamps) as 201
// Created inside the standard {"data": {...}} envelope.
func CreateAgentMemory(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var body mobilev1.CreateAgentMemoryRequest
		if err := DecodeProtoJSON(r, &body); err != nil {
			WriteError(w, merrors.ErrInvalidInput)
			return
		}

		if strings.TrimSpace(body.GetKey()) == "" || len(body.GetKey()) > MaxAgentMemoryKeyLength {
			WriteError(w, merrors.ErrAgentMemoryFieldTooLong)
			return
		}
		if strings.TrimSpace(body.GetContent()) == "" || len(body.GetContent()) > MaxAgentMemoryContentLength {
			WriteError(w, merrors.ErrAgentMemoryFieldTooLong)
			return
		}
		if len(body.GetCategory()) > MaxAgentMemoryCategoryLength {
			WriteError(w, merrors.ErrAgentMemoryFieldTooLong)
			return
		}

		created, mErr := c.AgentService.CreateAgentMemory(r.Context(), &orchestratorservice.CreateAgentMemoryOpts{
			UserID:   user.ID,
			AgentID:  chi.URLParam(r, "id"),
			Key:      body.GetKey(),
			Content:  body.GetContent(),
			Category: body.GetCategory(),
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteProtoSuccess(w, http.StatusCreated, &mobilev1.AgentMemoryResponse{
			Key:       created.Key,
			Content:   created.Content,
			Category:  created.Category,
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
		})
	}
}
