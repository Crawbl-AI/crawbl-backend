package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// Agent memory field length limits — mirror the MCP path enforcement.
const (
	MaxAgentMemoryKeyLength      = 256
	MaxAgentMemoryContentLength  = 16 * 1024 // 16 KiB
	MaxAgentMemoryCategoryLength = 128
)

// agentByIDFetcher wires a handler that looks up an agent by URL :id,
// calls the caller-supplied service method with {UserID, AgentID}, and
// converts the domain result to a response DTO. Collapses the shared
// GetAgent / GetAgentDetails scaffolding into one helper parameterised
// over domain and response types.
func agentByIDFetcher[Domain any, Response any](
	c *Context,
	fetch func(ctx context.Context, userID, agentID string) (Domain, *merrors.Error),
	toResponse func(Domain) Response,
) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) (Response, *merrors.Error) {
		var zero Response
		domain, mErr := fetch(r.Context(), deps.User.ID, chi.URLParam(r, "id"))
		if mErr != nil {
			return zero, mErr
		}
		return toResponse(domain), nil
	})
}

// GetAgent retrieves a single agent by ID.
// The agent must belong to a workspace owned by the authenticated user.
func GetAgent(c *Context) http.HandlerFunc {
	return agentByIDFetcher(c,
		func(ctx context.Context, userID, agentID string) (*orchestrator.Agent, *merrors.Error) {
			return c.AgentService.GetAgent(ctx, &orchestratorservice.GetAgentOpts{
				UserID: userID, AgentID: agentID,
			})
		},
		dto.ToAgentResponse,
	)
}

// GetAgentDetails retrieves full agent profile including stats.
// The agent must belong to a workspace owned by the authenticated user.
func GetAgentDetails(c *Context) http.HandlerFunc {
	return agentByIDFetcher(c,
		func(ctx context.Context, userID, agentID string) (*orchestrator.AgentDetails, *merrors.Error) {
			return c.AgentService.GetAgentDetails(ctx, &orchestratorservice.GetAgentDetailsOpts{
				UserID: userID, AgentID: agentID,
			})
		},
		dto.ToAgentDetailResponse,
	)
}

// GetAgentHistory retrieves paginated conversation history for an agent.
func GetAgentHistory(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) (dto.AgentHistoryResponse, *merrors.Error) {
		limit, offset := Pagination(r)

		items, pagination, mErr := c.AgentService.GetAgentHistory(r.Context(), &orchestratorservice.GetAgentHistoryOpts{
			UserID:  deps.User.ID,
			AgentID: chi.URLParam(r, "id"),
			Limit:   limit,
			Offset:  offset,
		})
		if mErr != nil {
			return dto.AgentHistoryResponse{}, mErr
		}

		historyItems := make([]dto.AgentHistoryItemResponse, 0, len(items))
		for _, item := range items {
			h := dto.AgentHistoryItemResponse{
				ConversationID: item.ConversationID,
				Title:          item.Title,
				Subtitle:       item.Subtitle,
			}
			if item.CreatedAt != nil {
				t := item.CreatedAt.Format("2006-01-02T15:04:05Z07:00")
				h.CreatedAt = &t
			}
			historyItems = append(historyItems, h)
		}

		return dto.AgentHistoryResponse{
			Items:      historyItems,
			Pagination: dto.NewOffsetPaginationResponse(*pagination),
		}, nil
	})
}

// GetAgentSettings retrieves model and prompt settings for an agent.
func GetAgentSettings(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) (dto.AgentSettingsResponse, *merrors.Error) {
		settings, mErr := c.AgentService.GetAgentSettings(r.Context(), &orchestratorservice.GetAgentSettingsOpts{
			UserID:  deps.User.ID,
			AgentID: chi.URLParam(r, "id"),
		})
		if mErr != nil {
			return dto.AgentSettingsResponse{}, mErr
		}

		prompts := make([]dto.AgentPromptResponse, 0, len(settings.Prompts))
		for _, p := range settings.Prompts {
			prompts = append(prompts, dto.AgentPromptResponse{
				ID:          p.ID,
				Name:        p.Name,
				Description: p.Description,
				Content:     p.Content,
			})
		}

		return dto.AgentSettingsResponse{
			Model:          settings.Model.ID,
			ResponseLength: string(settings.ResponseLength),
			Prompts:        prompts,
		}, nil
	})
}

// GetAgentTools retrieves the tools assigned to an agent with offset pagination.
func GetAgentTools(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) (dto.AgentToolsResponse, *merrors.Error) {
		limit, offset := Pagination(r)

		page, mErr := c.AgentService.GetAgentTools(r.Context(), &orchestratorservice.GetAgentToolsOpts{
			UserID:  deps.User.ID,
			AgentID: chi.URLParam(r, "id"),
			Limit:   limit,
			Offset:  offset,
		})
		if mErr != nil {
			return dto.AgentToolsResponse{}, mErr
		}

		tools := make([]dto.AgentToolResponse, 0, len(page.Data))
		for _, t := range page.Data {
			tools = append(tools, dto.ToAgentToolResponse(t))
		}

		return dto.AgentToolsResponse{
			Tools:      tools,
			Pagination: dto.NewOffsetPaginationResponse(page.Pagination),
		}, nil
	})
}

// ListModels returns the list of available LLM models.
// This is a public endpoint — no auth required (loaded by DictService before login).
func ListModels(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		available := orchestrator.GetAvailableModels()
		models := make([]dto.AgentModelResponse, 0, len(available))
		for _, m := range available {
			models = append(models, dto.AgentModelResponse{
				ID:          m.ID,
				Name:        m.Name,
				Description: m.Description,
			})
		}

		WriteSuccess(w, http.StatusOK, models)
	}
}

// GetAgentMemories retrieves memories from the agent's memory palace.
// Pagination is handled by the service/repo layer; the response is a flat
// array of memory entries inside the standard {"data": [...]} envelope.
func GetAgentMemories(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) ([]dto.AgentMemoryResponse, *merrors.Error) {
		category := r.URL.Query().Get("category")
		limit, offset := Pagination(r)

		memories, mErr := c.AgentService.GetAgentMemories(r.Context(), &orchestratorservice.GetAgentMemoriesOpts{
			UserID:   deps.User.ID,
			AgentID:  chi.URLParam(r, "id"),
			Category: category,
			Limit:    limit,
			Offset:   offset,
		})
		if mErr != nil {
			return nil, mErr
		}

		items := make([]dto.AgentMemoryResponse, 0, len(memories))
		for _, m := range memories {
			items = append(items, dto.AgentMemoryResponse{
				Key:       m.Key,
				Content:   m.Content,
				Category:  m.Category,
				CreatedAt: m.CreatedAt,
				UpdatedAt: m.UpdatedAt,
			})
		}

		return items, nil
	})
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
	return AuthedHandlerCreated(c, func(r *http.Request, deps *AuthedHandlerDeps, body *dto.CreateAgentMemoryRequest) (dto.AgentMemoryResponse, *merrors.Error) {
		if strings.TrimSpace(body.Key) == "" || len(body.Key) > MaxAgentMemoryKeyLength {
			return dto.AgentMemoryResponse{}, merrors.ErrAgentMemoryFieldTooLong
		}
		if strings.TrimSpace(body.Content) == "" || len(body.Content) > MaxAgentMemoryContentLength {
			return dto.AgentMemoryResponse{}, merrors.ErrAgentMemoryFieldTooLong
		}
		if len(body.Category) > MaxAgentMemoryCategoryLength {
			return dto.AgentMemoryResponse{}, merrors.ErrAgentMemoryFieldTooLong
		}

		created, mErr := c.AgentService.CreateAgentMemory(r.Context(), &orchestratorservice.CreateAgentMemoryOpts{
			UserID:   deps.User.ID,
			AgentID:  chi.URLParam(r, "id"),
			Key:      body.Key,
			Content:  body.Content,
			Category: body.Category,
		})
		if mErr != nil {
			return dto.AgentMemoryResponse{}, mErr
		}

		return dto.AgentMemoryResponse{
			Key:       created.Key,
			Content:   created.Content,
			Category:  created.Category,
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
		}, nil
	})
}
