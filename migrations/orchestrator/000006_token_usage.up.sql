-- Token Usage Tracking System
-- =============================================================================

-- Model Pricing (populated by K8s CronJob from AWS Pricing API + LiteLLM)
-- One row per (provider, model, region, effective_at). Old rows preserved for
-- historical cost computation. Never updated — new rows inserted on price change.
CREATE TABLE IF NOT EXISTS model_pricing (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    region TEXT NOT NULL DEFAULT 'global',
    input_cost_per_token NUMERIC(18,12) NOT NULL,
    output_cost_per_token NUMERIC(18,12) NOT NULL,
    cached_cost_per_token NUMERIC(18,12) NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'manual',
    effective_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_model_pricing_lookup
    ON model_pricing(provider, model, region, effective_at DESC);

-- Usage Plans (reference table, seeded from usage_plans.json on startup)
CREATE TABLE IF NOT EXISTS usage_plans (
    plan_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    monthly_token_limit BIGINT NOT NULL DEFAULT 500000,
    daily_request_limit INT,
    max_tokens_per_request INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Usage Quotas (per-user assignment, references usage_plans for limits)
CREATE TABLE IF NOT EXISTS usage_quotas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id TEXT NOT NULL DEFAULT 'free' REFERENCES usage_plans(plan_id),
    effective_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_quotas_user_active
    ON usage_quotas(user_id, plan_id)
    WHERE expires_at IS NULL;

-- Usage Counters (running totals, one row per user per month)
CREATE TABLE IF NOT EXISTS usage_counters (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    period TEXT NOT NULL,
    tokens_used BIGINT NOT NULL DEFAULT 0,
    prompt_tokens_used BIGINT NOT NULL DEFAULT 0,
    completion_tokens_used BIGINT NOT NULL DEFAULT 0,
    cost_usd NUMERIC(12,6) NOT NULL DEFAULT 0,
    request_count INT NOT NULL DEFAULT 0,
    last_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, period)
);

-- Agent Usage Counters (lifetime totals per agent)
CREATE TABLE IF NOT EXISTS agent_usage_counters (
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    tokens_used BIGINT NOT NULL DEFAULT 0,
    prompt_tokens_used BIGINT NOT NULL DEFAULT 0,
    completion_tokens_used BIGINT NOT NULL DEFAULT 0,
    cost_usd NUMERIC(12,6) NOT NULL DEFAULT 0,
    request_count INT NOT NULL DEFAULT 0,
    last_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_usage_counters_workspace
    ON agent_usage_counters(workspace_id);
