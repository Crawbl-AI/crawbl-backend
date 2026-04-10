// Package usagerepo provides persistence for token usage tracking and quota enforcement.
package usagerepo

import (
	"context"
	"fmt"

	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// Repo provides token usage tracking and quota enforcement operations.
type Repo interface {
	// CheckQuota returns the current token usage and the monthly limit for a user.
	// Returns (tokensUsed, monthlyLimit, error). If no quota row exists, returns 0, 0, nil
	// which callers should treat as "no quota configured" (allow).
	CheckQuota(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, period string) (int64, int64, *merrors.Error)

	// IncrementUsage atomically increments the user's usage counters for the given period.
	// Uses INSERT ON CONFLICT for first-of-period auto-creation.
	IncrementUsage(ctx context.Context, sess orchestratorrepo.SessionRunner, opts *IncrementUsageOpts) *merrors.Error

	// IncrementAgentUsage atomically increments an agent's lifetime usage counters.
	IncrementAgentUsage(ctx context.Context, sess orchestratorrepo.SessionRunner, opts *IncrementAgentUsageOpts) *merrors.Error

	// GetAgentUsage returns the lifetime usage stats for an agent.
	GetAgentUsage(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*AgentUsageRow, *merrors.Error)

	// GetUserUsage returns detailed usage counters for a user and billing period.
	// If no counter row exists, returns a zero-value row (no error).
	GetUserUsage(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, period string) (*UserUsageRow, *merrors.Error)

	// GetWorkspaceUsage returns aggregated usage counters for a specific workspace,
	// summed across all agents in that workspace. If no counters exist, returns a
	// zero-value row (no error).
	GetWorkspaceUsage(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) (*WorkspaceUsageRow, *merrors.Error)
}

// IncrementUsageOpts carries the token counts to add to a user's period counter.
type IncrementUsageOpts struct {
	UserID           string
	Period           string // "YYYY-MM"
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	CostUSD          float64
}

// IncrementAgentUsageOpts carries the token counts to add to an agent's lifetime counter.
type IncrementAgentUsageOpts struct {
	AgentID          string
	WorkspaceID      string
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	CostUSD          float64
}

// AgentUsageRow represents an agent's lifetime usage stats.
type AgentUsageRow struct {
	AgentID              string  `db:"agent_id"`
	TokensUsed           int64   `db:"tokens_used"`
	PromptTokensUsed     int64   `db:"prompt_tokens_used"`
	CompletionTokensUsed int64   `db:"completion_tokens_used"`
	CostUSD              float64 `db:"cost_usd"`
	RequestCount         int     `db:"request_count"`
}

// UserUsageRow represents a user's usage counters for a billing period.
type UserUsageRow struct {
	TokensUsed           int64   `db:"tokens_used"`
	PromptTokensUsed     int64   `db:"prompt_tokens_used"`
	CompletionTokensUsed int64   `db:"completion_tokens_used"`
	CostUSD              float64 `db:"cost_usd"`
	RequestCount         int     `db:"request_count"`
	PlanID               string  `db:"plan_id"`
}

// WorkspaceUsageRow represents aggregated usage counters for a workspace,
// summed across all agents that belong to it.
type WorkspaceUsageRow struct {
	TokensUsed           int64 `db:"tokens_used"`
	PromptTokensUsed     int64 `db:"prompt_tokens_used"`
	CompletionTokensUsed int64 `db:"completion_tokens_used"`
	RequestCount         int   `db:"request_count"`
}

type postgres struct{}

// New returns a Postgres-backed usage repository.
func New() Repo { return &postgres{} }

func (p *postgres) CheckQuota(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, period string) (int64, int64, *merrors.Error) {
	var result struct {
		TokensUsed int64 `db:"tokens_used"`
		TokenLimit int64 `db:"token_limit"`
	}

	// Join usage_counters with usage_quotas + usage_plans to get current usage and limit.
	// If no counter row exists, tokens_used defaults to 0.
	// If no quota row exists, the query returns no rows and we return 0, 0, nil (allow).
	err := sess.SelectBySql(`
		SELECT COALESCE(uc.tokens_used, 0) AS tokens_used,
		       COALESCE(up.monthly_token_limit, 0) AS token_limit
		FROM usage_quotas uq
		JOIN usage_plans up ON up.plan_id = uq.plan_id
		LEFT JOIN usage_counters uc ON uc.user_id = uq.user_id AND uc.period = ?
		WHERE uq.user_id = ?
		  AND (uq.expires_at IS NULL OR uq.expires_at > NOW())
		ORDER BY uq.effective_at DESC
		LIMIT 1`, period, userID).
		LoadOneContext(ctx, &result)

	if err != nil {
		if database.IsRecordNotFoundError(err) {
			// No quota configured — return 0, 0 so callers can allow.
			return 0, 0, nil
		}
		return 0, 0, merrors.WrapStdServerError(err, "usagerepo: check quota")
	}

	return result.TokensUsed, result.TokenLimit, nil
}

func (p *postgres) IncrementUsage(ctx context.Context, sess orchestratorrepo.SessionRunner, opts *IncrementUsageOpts) *merrors.Error {
	if opts == nil {
		return merrors.ErrInvalidInput
	}

	_, err := sess.InsertBySql(
		`INSERT INTO usage_counters (user_id, period, tokens_used, prompt_tokens_used, completion_tokens_used, cost_usd, request_count, last_updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 1, NOW())
		 ON CONFLICT (user_id, period) DO UPDATE SET
			tokens_used = usage_counters.tokens_used + EXCLUDED.tokens_used,
			prompt_tokens_used = usage_counters.prompt_tokens_used + EXCLUDED.prompt_tokens_used,
			completion_tokens_used = usage_counters.completion_tokens_used + EXCLUDED.completion_tokens_used,
			cost_usd = usage_counters.cost_usd + EXCLUDED.cost_usd,
			request_count = usage_counters.request_count + 1,
			last_updated_at = NOW()`,
		opts.UserID, opts.Period, opts.TotalTokens, opts.PromptTokens, opts.CompletionTokens, opts.CostUSD,
	).ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, fmt.Sprintf("usagerepo: increment usage for user %s period %s", opts.UserID, opts.Period))
	}

	return nil
}

func (p *postgres) IncrementAgentUsage(ctx context.Context, sess orchestratorrepo.SessionRunner, opts *IncrementAgentUsageOpts) *merrors.Error {
	if opts == nil {
		return merrors.ErrInvalidInput
	}

	_, err := sess.InsertBySql(
		`INSERT INTO agent_usage_counters (agent_id, workspace_id, tokens_used, prompt_tokens_used, completion_tokens_used, cost_usd, request_count, last_updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 1, NOW())
		 ON CONFLICT (agent_id) DO UPDATE SET
			tokens_used = agent_usage_counters.tokens_used + EXCLUDED.tokens_used,
			prompt_tokens_used = agent_usage_counters.prompt_tokens_used + EXCLUDED.prompt_tokens_used,
			completion_tokens_used = agent_usage_counters.completion_tokens_used + EXCLUDED.completion_tokens_used,
			cost_usd = agent_usage_counters.cost_usd + EXCLUDED.cost_usd,
			request_count = agent_usage_counters.request_count + 1,
			last_updated_at = NOW()`,
		opts.AgentID, opts.WorkspaceID, opts.TotalTokens, opts.PromptTokens, opts.CompletionTokens, opts.CostUSD,
	).ExecContext(ctx)
	if err != nil {
		return merrors.WrapStdServerError(err, fmt.Sprintf("usagerepo: increment agent usage for %s", opts.AgentID))
	}

	return nil
}

func (p *postgres) GetAgentUsage(ctx context.Context, sess orchestratorrepo.SessionRunner, agentID string) (*AgentUsageRow, *merrors.Error) {
	if agentID == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row AgentUsageRow
	err := sess.Select(
		"agent_id",
		"tokens_used",
		"prompt_tokens_used",
		"completion_tokens_used",
		"cost_usd",
		"request_count",
	).From("agent_usage_counters").
		Where("agent_id = ?", agentID).
		LoadOneContext(ctx, &row)

	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return &AgentUsageRow{AgentID: agentID}, nil
		}
		return nil, merrors.WrapStdServerError(err, fmt.Sprintf("usagerepo: get agent usage for %s", agentID))
	}

	return &row, nil
}

func (p *postgres) GetUserUsage(ctx context.Context, sess orchestratorrepo.SessionRunner, userID, period string) (*UserUsageRow, *merrors.Error) {
	if userID == "" || period == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row UserUsageRow
	err := sess.SelectBySql(`
		SELECT COALESCE(uc.tokens_used, 0)            AS tokens_used,
		       COALESCE(uc.prompt_tokens_used, 0)      AS prompt_tokens_used,
		       COALESCE(uc.completion_tokens_used, 0)  AS completion_tokens_used,
		       COALESCE(uc.cost_usd, 0)                AS cost_usd,
		       COALESCE(uc.request_count, 0)           AS request_count,
		       COALESCE(uq.plan_id, '')                AS plan_id
		FROM usage_quotas uq
		LEFT JOIN usage_counters uc ON uc.user_id = uq.user_id AND uc.period = ?
		WHERE uq.user_id = ?
		  AND (uq.expires_at IS NULL OR uq.expires_at > NOW())
		ORDER BY uq.effective_at DESC
		LIMIT 1`, period, userID).
		LoadOneContext(ctx, &row)

	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return &UserUsageRow{}, nil
		}
		return nil, merrors.WrapStdServerError(err, fmt.Sprintf("usagerepo: get user usage for %s period %s", userID, period))
	}

	return &row, nil
}

func (p *postgres) GetWorkspaceUsage(ctx context.Context, sess orchestratorrepo.SessionRunner, workspaceID string) (*WorkspaceUsageRow, *merrors.Error) {
	if workspaceID == "" {
		return nil, merrors.ErrInvalidInput
	}

	var row WorkspaceUsageRow
	err := sess.Select(
		"COALESCE(SUM(tokens_used), 0) AS tokens_used",
		"COALESCE(SUM(prompt_tokens_used), 0) AS prompt_tokens_used",
		"COALESCE(SUM(completion_tokens_used), 0) AS completion_tokens_used",
		"COALESCE(SUM(request_count), 0) AS request_count",
	).
		From("agent_usage_counters").
		Where("workspace_id = ?", workspaceID).
		LoadOneContext(ctx, &row)

	if err != nil {
		if database.IsRecordNotFoundError(err) {
			return &WorkspaceUsageRow{}, nil
		}
		return nil, merrors.WrapStdServerError(err, fmt.Sprintf("usagerepo: get workspace usage for %s", workspaceID))
	}

	return &row, nil
}
