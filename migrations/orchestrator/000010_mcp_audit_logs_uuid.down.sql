-- Reverse: drop FKs, convert columns back to TEXT NOT NULL.
-- Note: NULL audit rows (where user was deleted post-migration) will become empty strings.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_mcp_audit_logs_workspace') THEN
        ALTER TABLE mcp_audit_logs DROP CONSTRAINT fk_mcp_audit_logs_workspace;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_mcp_audit_logs_user') THEN
        ALTER TABLE mcp_audit_logs DROP CONSTRAINT fk_mcp_audit_logs_user;
    END IF;
END $$;

-- Backfill NULLs as empty strings before re-adding NOT NULL.
UPDATE mcp_audit_logs SET user_id      = '00000000-0000-0000-0000-000000000000' WHERE user_id      IS NULL;
UPDATE mcp_audit_logs SET workspace_id = '00000000-0000-0000-0000-000000000000' WHERE workspace_id IS NULL;

ALTER TABLE mcp_audit_logs
    ALTER COLUMN user_id      TYPE TEXT USING user_id::TEXT,
    ALTER COLUMN workspace_id TYPE TEXT USING workspace_id::TEXT,
    ALTER COLUMN user_id      SET NOT NULL,
    ALTER COLUMN workspace_id SET NOT NULL;
