-- Migrate mcp_audit_logs.user_id and workspace_id from TEXT to UUID with FKs.
--
-- Safety:
--   1. Pre-DELETE uses a case-insensitive regex so uppercase UUIDs are preserved.
--   2. ALTER COLUMN and ADD CONSTRAINT are wrapped in information_schema / pg_constraint
--      guards so the migration is idempotent across retries.
--   3. FKs use ON DELETE SET NULL so the audit trail survives user/workspace deletion
--      (compliance requirement).

-- 1. Drop rows that cannot be cast to UUID (case-insensitive match).
DELETE FROM mcp_audit_logs
WHERE user_id      !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR workspace_id !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';

-- 2. Convert columns to UUID, only if not already UUID. Also drop NOT NULL so
--    ON DELETE SET NULL can work.
DO $$
BEGIN
    IF (SELECT data_type FROM information_schema.columns
        WHERE table_name = 'mcp_audit_logs' AND column_name = 'user_id') <> 'uuid' THEN
        ALTER TABLE mcp_audit_logs
            ALTER COLUMN user_id TYPE UUID USING user_id::UUID,
            ALTER COLUMN user_id DROP NOT NULL;
    END IF;

    IF (SELECT data_type FROM information_schema.columns
        WHERE table_name = 'mcp_audit_logs' AND column_name = 'workspace_id') <> 'uuid' THEN
        ALTER TABLE mcp_audit_logs
            ALTER COLUMN workspace_id TYPE UUID USING workspace_id::UUID,
            ALTER COLUMN workspace_id DROP NOT NULL;
    END IF;
END $$;

-- 3. Add FKs with ON DELETE SET NULL — guarded against duplicate constraint errors.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_mcp_audit_logs_user') THEN
        ALTER TABLE mcp_audit_logs
            ADD CONSTRAINT fk_mcp_audit_logs_user
                FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_mcp_audit_logs_workspace') THEN
        ALTER TABLE mcp_audit_logs
            ADD CONSTRAINT fk_mcp_audit_logs_workspace
                FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE SET NULL;
    END IF;
END $$;
