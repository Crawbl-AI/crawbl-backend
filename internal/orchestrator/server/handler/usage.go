package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
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
		sess := database.SessionFromContext(r.Context())

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

		WriteProtoSuccess(w, http.StatusOK, &mobilev1.UserUsageSummaryResponse{
			CurrentPeriod:        period,
			TokensUsed:           int32(tokensUsed),                    // #nosec G115 -- token count within int32 range for display
			PromptTokensUsed:     int32(counters.PromptTokensUsed),     // #nosec G115 -- token count within int32 range for display
			CompletionTokensUsed: int32(counters.CompletionTokensUsed), // #nosec G115 -- token count within int32 range for display
			RequestCount:         int32(counters.RequestCount),         // #nosec G115 -- request count fits in int32
			TokenLimit:           int32(tokenLimit),                    // #nosec G115 -- token limit within int32 range for display
			PlanId:               counters.PlanID,
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
		sess := database.SessionFromContext(r.Context())

		// Verify the workspace belongs to this user.
		if _, mErr := c.WorkspaceService.GetByID(r.Context(), &orchestratorservice.GetWorkspaceOpts{
			UserID:      user.ID,
			WorkspaceID: workspaceID,
		}); mErr != nil {
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

		WriteProtoSuccess(w, http.StatusOK, &mobilev1.WorkspaceUsageResponse{
			Period:               period,
			TokensUsed:           int32(counters.TokensUsed),           // #nosec G115 -- token count within int32 range for display
			PromptTokensUsed:     int32(counters.PromptTokensUsed),     // #nosec G115 -- token count within int32 range for display
			CompletionTokensUsed: int32(counters.CompletionTokensUsed), // #nosec G115 -- token count within int32 range for display
			RequestCount:         int32(counters.RequestCount),         // #nosec G115 -- request count fits in int32
			TokenLimit:           int32(tokenLimit),                    // #nosec G115 -- token limit within int32 range for display
		})
	}
}
