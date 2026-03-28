-- MCP tool call audit log for ISO 27001 compliance.
-- Every MCP tool invocation is recorded with full input/output for traceability.
CREATE TABLE IF NOT EXISTS mcp_audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL,
    input JSONB NOT NULL DEFAULT '{}'::jsonb,
    output JSONB,
    error_message TEXT,
    success BOOLEAN NOT NULL DEFAULT true,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    remote_addr TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for querying audit logs by user (compliance reports).
CREATE INDEX IF NOT EXISTS idx_mcp_audit_logs_user_id ON mcp_audit_logs(user_id, created_at DESC);
-- Index for querying audit logs by tool (usage analytics).
CREATE INDEX IF NOT EXISTS idx_mcp_audit_logs_tool_name ON mcp_audit_logs(tool_name, created_at DESC);
-- Index for querying by workspace (workspace-scoped audits).
CREATE INDEX IF NOT EXISTS idx_mcp_audit_logs_workspace_id ON mcp_audit_logs(workspace_id, created_at DESC);
