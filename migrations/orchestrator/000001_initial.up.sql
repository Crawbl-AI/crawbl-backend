-- Users and preferences
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    subject TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL,
    nickname TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    surname TEXT NOT NULL DEFAULT '',
    avatar_url TEXT NULL,
    country_code TEXT NULL,
    date_of_birth TIMESTAMPTZ NULL,
    is_banned BOOLEAN NOT NULL DEFAULT FALSE,
    has_agreed_with_terms BOOLEAN NOT NULL DEFAULT FALSE,
    has_agreed_with_privacy_policy BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_users_subject ON users(subject);

CREATE TABLE IF NOT EXISTS user_preferences (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    platform_theme TEXT NULL,
    platform_language TEXT NULL,
    currency_code TEXT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS user_push_tokens (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    push_token TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- Workspaces
CREATE TABLE IF NOT EXISTS workspaces (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_workspaces_user_id ON workspaces(user_id);

-- Agents
CREATE TABLE IF NOT EXISTS agents (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    slug TEXT NOT NULL,
    avatar_url TEXT NOT NULL DEFAULT '',
    system_prompt TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_workspace_slug ON agents(workspace_id, slug);
CREATE INDEX IF NOT EXISTS idx_agents_workspace_id ON agents(workspace_id);

-- Tools catalog
CREATE TABLE IF NOT EXISTS tools (
    name TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL,
    icon_url TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Agent settings
CREATE TABLE IF NOT EXISTS agent_settings (
    agent_id UUID PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    model TEXT NOT NULL DEFAULT 'auto',
    response_length TEXT NOT NULL DEFAULT 'auto',
    allowed_tools TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- Agent prompts
CREATE TABLE IF NOT EXISTS agent_prompts (
    id UUID PRIMARY KEY,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_prompts_agent_id ON agent_prompts(agent_id);

-- Conversations
CREATE TABLE IF NOT EXISTS conversations (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id UUID NULL REFERENCES agents(id) ON DELETE SET NULL,
    type TEXT NOT NULL,
    title TEXT NOT NULL,
    unread_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_conversations_workspace_id ON conversations(workspace_id);
CREATE INDEX IF NOT EXISTS idx_conversations_workspace_type ON conversations(workspace_id, type);

-- Agent history (Manager-created events)
CREATE TABLE IF NOT EXISTS agent_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    conversation_id UUID NULL REFERENCES conversations(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    subtitle TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_history_agent_id ON agent_history(agent_id, created_at DESC);

-- Messages
CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content JSONB NOT NULL,
    status TEXT NOT NULL,
    local_id TEXT NULL,
    agent_id UUID NULL REFERENCES agents(id) ON DELETE SET NULL,
    attachments JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation_created_at ON messages(conversation_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_messages_local_id ON messages(local_id);
CREATE INDEX IF NOT EXISTS idx_messages_agent_id ON messages(agent_id);

-- MCP audit log
CREATE TABLE IF NOT EXISTS mcp_audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    workspace_id UUID REFERENCES workspaces(id) ON DELETE SET NULL,
    session_id TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL,
    input JSONB NOT NULL DEFAULT '{}'::jsonb,
    output JSONB,
    error_message TEXT,
    success BOOLEAN NOT NULL DEFAULT true,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    remote_addr TEXT NOT NULL DEFAULT '',
    api_calls JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mcp_audit_logs_user_id ON mcp_audit_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_mcp_audit_logs_tool_name ON mcp_audit_logs(tool_name, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_mcp_audit_logs_workspace_id ON mcp_audit_logs(workspace_id, created_at DESC);

-- Integration connections
CREATE TABLE IF NOT EXISTS integration_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    access_token_encrypted TEXT,
    refresh_token_encrypted TEXT,
    token_expires_at TIMESTAMPTZ,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    provider_user_id TEXT,
    provider_user_email TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_integration_connections_unique
    ON integration_connections(user_id, workspace_id, provider)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_integration_connections_user_workspace
    ON integration_connections(user_id, workspace_id);

CREATE INDEX IF NOT EXISTS idx_integration_connections_provider
    ON integration_connections(provider, created_at DESC);

-- Multi-Agent System Tables

-- Agent delegation audit log.
-- Tracks every delegation event observed in streaming responses.
CREATE TABLE IF NOT EXISTS agent_delegations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    trigger_message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    delegator_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    delegate_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL DEFAULT 'delegate',
    task_summary TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'running',
    duration_ms INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_delegations_workspace ON agent_delegations(workspace_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_delegations_conversation ON agent_delegations(conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_delegations_trigger_message ON agent_delegations(trigger_message_id);

-- Inter-agent messages routed through the orchestrator.
CREATE TABLE IF NOT EXISTS agent_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    root_message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
    from_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    from_agent_slug TEXT NOT NULL,
    to_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    to_agent_slug TEXT NOT NULL,
    request_text TEXT NOT NULL,
    response_text TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT,
    duration_ms INTEGER,
    depth INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_messages_workspace ON agent_messages(workspace_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_messages_conversation ON agent_messages(conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_messages_from_agent ON agent_messages(from_agent_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_messages_to_agent ON agent_messages(to_agent_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_messages_root_message ON agent_messages(root_message_id);

-- Shared artifacts created and modified by agents.
CREATE TABLE IF NOT EXISTS artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'text/markdown',
    current_version INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'draft',
    created_by_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_artifacts_workspace ON artifacts(workspace_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_artifacts_conversation ON artifacts(conversation_id);

-- Artifact versions (immutable log of changes).
CREATE TABLE IF NOT EXISTS artifact_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    content TEXT NOT NULL,
    change_summary TEXT NOT NULL DEFAULT '',
    agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    agent_slug TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_artifact_versions_unique ON artifact_versions(artifact_id, version);
CREATE INDEX IF NOT EXISTS idx_artifact_versions_artifact ON artifact_versions(artifact_id, version DESC);

-- Artifact reviews.
CREATE TABLE IF NOT EXISTS artifact_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    reviewer_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    reviewer_agent_slug TEXT NOT NULL,
    outcome TEXT NOT NULL,
    comments TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_artifact_reviews_artifact ON artifact_reviews(artifact_id, version);

-- Workflow definitions.
CREATE TABLE IF NOT EXISTS workflow_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    steps JSONB NOT NULL DEFAULT '[]'::jsonb,
    trigger_policy TEXT NOT NULL DEFAULT 'any_agent',
    trigger_agents TEXT[] NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_definitions_workspace ON workflow_definitions(workspace_id);

-- Workflow executions (instances).
CREATE TABLE IF NOT EXISTS workflow_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_definition_id UUID NOT NULL REFERENCES workflow_definitions(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    current_step INTEGER NOT NULL DEFAULT 0,
    context JSONB NOT NULL DEFAULT '{}'::jsonb,
    triggered_by TEXT NOT NULL DEFAULT '',
    error_message TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_executions_workspace ON workflow_executions(workspace_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_executions_status ON workflow_executions(status) WHERE status IN ('pending', 'running', 'paused');

-- Workflow step executions.
CREATE TABLE IF NOT EXISTS workflow_step_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id UUID NOT NULL REFERENCES workflow_executions(id) ON DELETE CASCADE,
    step_index INTEGER NOT NULL,
    step_name TEXT NOT NULL,
    agent_slug TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    input_text TEXT NOT NULL DEFAULT '',
    output_text TEXT,
    artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
    duration_ms INTEGER,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_step_executions_execution ON workflow_step_executions(execution_id, step_index);

-- Scheduled agent triggers.
CREATE TABLE IF NOT EXISTS agent_triggers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    cron_expression TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    prompt TEXT NOT NULL,
    workflow_id UUID REFERENCES workflow_definitions(id) ON DELETE SET NULL,
    conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_triggered_at TIMESTAMPTZ,
    next_trigger_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_triggers_workspace ON agent_triggers(workspace_id);
CREATE INDEX IF NOT EXISTS idx_agent_triggers_next ON agent_triggers(next_trigger_at) WHERE is_active = TRUE;

-- Trigger execution log.
CREATE TABLE IF NOT EXISTS agent_trigger_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trigger_id UUID NOT NULL REFERENCES agent_triggers(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'running',
    result_text TEXT,
    duration_ms INTEGER,
    workflow_execution_id UUID REFERENCES workflow_executions(id) ON DELETE SET NULL,
    message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
    error_message TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_trigger_executions_trigger ON agent_trigger_executions(trigger_id, started_at DESC);

-- Memory-based triggers.
CREATE TABLE IF NOT EXISTS memory_triggers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    memory_category TEXT NOT NULL,
    match_pattern TEXT NOT NULL,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    prompt_template TEXT NOT NULL,
    cooldown_secs INTEGER NOT NULL DEFAULT 3600,
    last_triggered_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_memory_triggers_workspace ON memory_triggers(workspace_id) WHERE is_active = TRUE;

CREATE TABLE IF NOT EXISTS models (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tool_categories (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    image_url TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS integration_categories (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    image_url TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS integration_providers (
    provider TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    icon_url TEXT NOT NULL DEFAULT '',
    category_id TEXT NOT NULL DEFAULT '',
    is_enabled BOOLEAN NOT NULL DEFAULT false,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- MemPalace memory system: vector store, knowledge graph, identities.
-- Requires pgvector extension for semantic search.

CREATE EXTENSION IF NOT EXISTS vector SCHEMA public;

-- Core vector store — each drawer is a chunk of verbatim content with embedding.
CREATE TABLE IF NOT EXISTS memory_drawers (
    id            TEXT        PRIMARY KEY,
    workspace_id  UUID        NOT NULL,
    wing          TEXT        NOT NULL,
    room          TEXT        NOT NULL,
    hall          TEXT        NOT NULL DEFAULT '',
    content       TEXT        NOT NULL,
    embedding     vector(1536),
    importance    REAL        NOT NULL DEFAULT 3.0,
    memory_type   TEXT        NOT NULL DEFAULT '',
    source_file   TEXT        NOT NULL DEFAULT '',
    added_by      TEXT        NOT NULL DEFAULT 'system',
    filed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    state            TEXT NOT NULL DEFAULT 'raw',
    summary          TEXT NOT NULL DEFAULT '',
    added_by_agent   TEXT NOT NULL DEFAULT '',
    last_accessed_at TIMESTAMPTZ,
    access_count     INT  NOT NULL DEFAULT 0,
    superseded_by    TEXT,
    cluster_id       TEXT,
    retry_count      INT  NOT NULL DEFAULT 0,
    pipeline_tier TEXT NOT NULL DEFAULT 'llm',
    entity_count  INTEGER NOT NULL DEFAULT 0,
    triple_count  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_drawers_workspace      ON memory_drawers (workspace_id);
CREATE INDEX IF NOT EXISTS idx_drawers_workspace_wing ON memory_drawers (workspace_id, wing);
CREATE INDEX IF NOT EXISTS idx_drawers_workspace_room ON memory_drawers (workspace_id, wing, room);
-- NOTE: HNSW vector index removed — Bitnami PG image uses SIMD instructions
-- (AVX2) that Digital Ocean CPUs don't support, causing SIGILL crashes on INSERT.
-- At <10K drawers per workspace, sequential scan is fast enough.

-- Partial index for cold worker polling efficiency (covers 'raw' and 'classifying'
-- states for cold worker polling efficiency.
CREATE INDEX IF NOT EXISTS idx_drawers_state ON memory_drawers (state)
    WHERE state IN ('raw', 'classifying');

CREATE INDEX IF NOT EXISTS idx_drawers_superseded ON memory_drawers (superseded_by)
    WHERE superseded_by IS NOT NULL;

-- Enrichment sweep index.
CREATE INDEX IF NOT EXISTS idx_drawers_enrich
    ON memory_drawers (workspace_id, created_at)
    WHERE state = 'processed'
      AND pipeline_tier <> 'llm'
      AND entity_count = 0
      AND importance >= 3.0;

-- Knowledge graph: entity nodes.
CREATE TABLE IF NOT EXISTS memory_entities (
    id            TEXT        NOT NULL,
    workspace_id  UUID        NOT NULL,
    name          TEXT        NOT NULL,
    type          TEXT        NOT NULL DEFAULT 'unknown',
    properties    JSONB       NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    embedding     public.vector(1536),
    PRIMARY KEY (workspace_id, id)
);

-- Knowledge graph: temporal relationship triples.
CREATE TABLE IF NOT EXISTS memory_triples (
    id              TEXT        NOT NULL,
    workspace_id    UUID        NOT NULL,
    subject         TEXT        NOT NULL,
    predicate       TEXT        NOT NULL,
    object          TEXT        NOT NULL,
    valid_from      TEXT,
    valid_to        TEXT,
    confidence      REAL        NOT NULL DEFAULT 1.0,
    source_closet   TEXT        NOT NULL DEFAULT '',
    source_file     TEXT        NOT NULL DEFAULT '',
    extracted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, id)
);

CREATE INDEX IF NOT EXISTS idx_triples_subject   ON memory_triples (workspace_id, subject);
CREATE INDEX IF NOT EXISTS idx_triples_object    ON memory_triples (workspace_id, object);
CREATE INDEX IF NOT EXISTS idx_triples_predicate ON memory_triples (workspace_id, predicate);
CREATE INDEX IF NOT EXISTS idx_triples_validity  ON memory_triples (workspace_id, valid_from, valid_to);

-- Per-workspace L0 identity text.
CREATE TABLE IF NOT EXISTS memory_identities (
    workspace_id  UUID        PRIMARY KEY,
    content       TEXT        NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Model Pricing (populated by K8s CronJob from AWS Pricing API + LiteLLM)
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email);

-- memory_type_centroids stores one 1536-dim prototype vector per memory type.
-- Used by the autoingest worker for centroid-based classification.
CREATE TABLE IF NOT EXISTS memory_type_centroids (
    memory_type  TEXT         NOT NULL PRIMARY KEY,
    centroid     vector(1536) NOT NULL,
    sample_count INTEGER      NOT NULL,
    computed_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    source_hash  TEXT         NOT NULL
);

-- Index on users.nickname for CheckNicknameExists lookups during onboarding.
CREATE INDEX IF NOT EXISTS idx_users_nickname ON users(nickname);
