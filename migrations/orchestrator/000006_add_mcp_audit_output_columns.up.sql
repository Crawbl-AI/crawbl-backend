-- Add output and api_calls columns to mcp_audit_logs for tracking
-- what the orchestrator returned and which outgoing API calls it made.
ALTER TABLE mcp_audit_logs ADD COLUMN IF NOT EXISTS output JSONB;
ALTER TABLE mcp_audit_logs ADD COLUMN IF NOT EXISTS api_calls JSONB;
