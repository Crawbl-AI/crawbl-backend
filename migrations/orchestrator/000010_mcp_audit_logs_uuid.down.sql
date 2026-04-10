ALTER TABLE mcp_audit_logs
    DROP CONSTRAINT IF EXISTS fk_mcp_audit_logs_workspace,
    DROP CONSTRAINT IF EXISTS fk_mcp_audit_logs_user;

ALTER TABLE mcp_audit_logs
    ALTER COLUMN user_id      TYPE TEXT USING user_id::TEXT,
    ALTER COLUMN workspace_id TYPE TEXT USING workspace_id::TEXT;
