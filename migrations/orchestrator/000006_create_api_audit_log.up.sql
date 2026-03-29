-- API request audit log for ISO 27001 compliance.
-- Records every authenticated API request with method, path, user, and response status.
CREATE TABLE IF NOT EXISTS api_audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_subject TEXT NOT NULL,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    user_agent TEXT NOT NULL DEFAULT '',
    remote_addr TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_audit_logs_user ON api_audit_logs(user_subject, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_audit_logs_path ON api_audit_logs(path, created_at DESC);
