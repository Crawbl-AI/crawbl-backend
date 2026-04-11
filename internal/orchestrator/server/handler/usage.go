package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// UserUsageSummary returns the authenticated user's monthly token usage summary.
// GET /v1/users/usage/summary
func UserUsageSummary(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) (dto.UserUsageSummaryResponse, *merrors.Error) {
		period := time.Now().UTC().Format("2006-01")

		tokensUsed, tokenLimit, mErr := c.UsageRepo.CheckQuota(r.Context(), deps.Sess, deps.User.ID, period)
		if mErr != nil {
			return dto.UserUsageSummaryResponse{}, mErr
		}

		counters, mErr := c.UsageRepo.GetUserUsage(r.Context(), deps.Sess, deps.User.ID, period)
		if mErr != nil {
			return dto.UserUsageSummaryResponse{}, mErr
		}

		return dto.UserUsageSummaryResponse{
			CurrentPeriod:        period,
			TokensUsed:           tokensUsed,
			PromptTokensUsed:     counters.PromptTokensUsed,
			CompletionTokensUsed: counters.CompletionTokensUsed,
			RequestCount:         counters.RequestCount,
			TokenLimit:           tokenLimit,
			PlanID:               counters.PlanID,
		}, nil
	})
}

// WorkspaceUsage returns token usage for a specific workspace for the current billing period.
// GET /v1/workspaces/{id}/usage
func WorkspaceUsage(c *Context) http.HandlerFunc {
	return AuthedHandlerNoBody(c, func(r *http.Request, deps *AuthedHandlerDeps) (dto.WorkspaceUsageResponse, *merrors.Error) {
		workspaceID := chi.URLParam(r, "id")
		period := time.Now().UTC().Format("2006-01")

		// Verify the workspace belongs to this user.
		if _, mErr := c.WorkspaceService.GetByID(r.Context(), &orchestratorservice.GetWorkspaceOpts{
			Sess:        deps.Sess,
			UserID:      deps.User.ID,
			WorkspaceID: workspaceID,
		}); mErr != nil {
			return dto.WorkspaceUsageResponse{}, mErr
		}

		_, tokenLimit, mErr := c.UsageRepo.CheckQuota(r.Context(), deps.Sess, deps.User.ID, period)
		if mErr != nil {
			return dto.WorkspaceUsageResponse{}, mErr
		}

		counters, mErr := c.UsageRepo.GetWorkspaceUsage(r.Context(), deps.Sess, workspaceID)
		if mErr != nil {
			return dto.WorkspaceUsageResponse{}, mErr
		}

		return dto.WorkspaceUsageResponse{
			Period:               period,
			TokensUsed:           counters.TokensUsed,
			PromptTokensUsed:     counters.PromptTokensUsed,
			CompletionTokensUsed: counters.CompletionTokensUsed,
			RequestCount:         counters.RequestCount,
			TokenLimit:           tokenLimit,
		}, nil
	})
}
