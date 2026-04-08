package dto

// WorkspaceUsageResponse is the response for GET /v1/workspaces/{id}/usage.
type WorkspaceUsageResponse struct {
	Period               string  `json:"period"`
	TokensUsed           int64   `json:"tokens_used"`
	PromptTokensUsed     int64   `json:"prompt_tokens_used"`
	CompletionTokensUsed int64   `json:"completion_tokens_used"`
	CostUSD              float64 `json:"cost_usd"`
	RequestCount         int     `json:"request_count"`
	TokenLimit           int64   `json:"token_limit"`
}

// UserUsageSummaryResponse is the response for GET /v1/users/usage/summary.
type UserUsageSummaryResponse struct {
	CurrentPeriod        string  `json:"current_period"`
	TokensUsed           int64   `json:"tokens_used"`
	PromptTokensUsed     int64   `json:"prompt_tokens_used"`
	CompletionTokensUsed int64   `json:"completion_tokens_used"`
	CostUSD              float64 `json:"cost_usd"`
	RequestCount         int     `json:"request_count"`
	TokenLimit           int64   `json:"token_limit"`
	PlanID               string  `json:"plan_id"`
}
