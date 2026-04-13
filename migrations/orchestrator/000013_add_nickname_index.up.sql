-- Index on users.nickname for CheckNicknameExists lookups during onboarding.
-- The column has no uniqueness constraint (nicknames can collide), but the
-- existence check queries WHERE nickname = $1 on every registration flow.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_nickname ON users(nickname);
