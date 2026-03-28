-- Integration connections store OAuth tokens for third-party services.
-- Each user can connect multiple providers (Slack, Gmail, Jira, etc.)
-- per workspace. Tokens are encrypted at rest.
--
-- The orchestrator uses these tokens when MCP tool calls require
-- API access to external services on behalf of the user.
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

-- One active connection per provider per user per workspace.
CREATE UNIQUE INDEX IF NOT EXISTS idx_integration_connections_unique
    ON integration_connections(user_id, workspace_id, provider)
    WHERE status = 'active';

-- Index for listing a user's connections.
CREATE INDEX IF NOT EXISTS idx_integration_connections_user_workspace
    ON integration_connections(user_id, workspace_id);

-- Index for provider-scoped queries (admin/audit).
CREATE INDEX IF NOT EXISTS idx_integration_connections_provider
    ON integration_connections(provider, created_at DESC);
