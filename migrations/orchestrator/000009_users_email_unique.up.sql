-- Dedupe case-insensitively before adding the unique constraint.
-- Without this, duplicate/case-variant rows would block autoMigrate on startup.
-- Keeps the OLDEST row per case-normalized email (earliest created_at wins).
DELETE FROM users u1
USING users u2
WHERE LOWER(u1.email) = LOWER(u2.email)
  AND (u1.created_at, u1.id) > (u2.created_at, u2.id);

-- Unique on the lower-cased email so "Alice@x.com" and "alice@x.com" cannot coexist.
-- Idempotent via IF NOT EXISTS.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_lower ON users (LOWER(email));
