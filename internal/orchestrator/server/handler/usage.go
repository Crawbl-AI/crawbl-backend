package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server/dto"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
)

// UserUsageSummary returns the authenticated user's monthly token usage summary.
// GET /v1/users/usage/summary
func UserUsageSummary(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		period := time.Now().UTC().Format("2006-01")
		sess := c.NewSession()

		tokensUsed, tokenLimit, mErr := c.UsageRepo.CheckQuota(r.Context(), sess, user.ID, period)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		counters, mErr := c.UsageRepo.GetUserUsage(r.Context(), sess, user.ID, period)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, dto.UserUsageSummaryResponse{
			CurrentPeriod:        period,
			TokensUsed:           tokensUsed,
			PromptTokensUsed:     counters.PromptTokensUsed,
			CompletionTokensUsed: counters.CompletionTokensUsed,
			RequestCount:         counters.RequestCount,
			TokenLimit:           tokenLimit,
			PlanID:               counters.PlanID,
		})
	}
}

// WorkspaceUsage returns token usage for a specific workspace for the current billing period.
// GET /v1/workspaces/{id}/usage
func WorkspaceUsage(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		workspaceID := chi.URLParam(r, "id")
		period := time.Now().UTC().Format("2006-01")
		sess := c.NewSession()

		// Verify the workspace belongs to this user.
		_, mErr = c.WorkspaceService.GetByID(r.Context(), &orchestratorservice.GetWorkspaceOpts{
			Sess:        sess,
			UserID:      user.ID,
			WorkspaceID: workspaceID,
		})
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		_, tokenLimit, mErr := c.UsageRepo.CheckQuota(r.Context(), sess, user.ID, period)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		counters, mErr := c.UsageRepo.GetWorkspaceUsage(r.Context(), sess, workspaceID)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, dto.WorkspaceUsageResponse{
			Period:               period,
			TokensUsed:           counters.TokensUsed,
			PromptTokensUsed:     counters.PromptTokensUsed,
			CompletionTokensUsed: counters.CompletionTokensUsed,
			RequestCount:         counters.RequestCount,
			TokenLimit:           tokenLimit,
		})
	}
}
