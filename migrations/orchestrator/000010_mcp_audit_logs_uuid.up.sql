-- Migrate mcp_audit_logs.user_id and workspace_id from TEXT to UUID and add FKs.
--
-- Safety: the ::UUID cast will fail if any row contains a non-UUID string.
-- Since this table was created with TEXT columns and no FK constraints, it may
-- hold arbitrary strings in dev environments. The DELETE below removes any rows
-- that cannot be safely cast to UUID before the ALTER TABLE runs.
--
-- If this migration fails on a production database, run the following first to
-- inspect bad rows:
--
--   SELECT id, user_id, workspace_id FROM mcp_audit_logs
--   WHERE user_id  !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
--      OR workspace_id !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';
--
-- Then run: DELETE FROM mcp_audit_logs; before re-running this migration.

DELETE FROM mcp_audit_logs
WHERE user_id      !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR workspace_id !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';

ALTER TABLE mcp_audit_logs
    ALTER COLUMN user_id      TYPE UUID USING user_id::UUID,
    ALTER COLUMN workspace_id TYPE UUID USING workspace_id::UUID;

ALTER TABLE mcp_audit_logs
    ADD CONSTRAINT fk_mcp_audit_logs_user
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE mcp_audit_logs
    ADD CONSTRAINT fk_mcp_audit_logs_workspace
        FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE;
