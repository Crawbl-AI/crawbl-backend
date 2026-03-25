CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    subject TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL,
    nickname TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    surname TEXT NOT NULL DEFAULT '',
    avatar_url TEXT NULL,
    country_code TEXT NULL,
    date_of_birth TIMESTAMPTZ NULL,
    is_banned BOOLEAN NOT NULL DEFAULT FALSE,
    has_agreed_with_terms BOOLEAN NOT NULL DEFAULT FALSE,
    has_agreed_with_privacy_policy BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_users_subject ON users(subject);

CREATE TABLE IF NOT EXISTS user_preferences (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    platform_theme TEXT NULL,
    platform_language TEXT NULL,
    currency_code TEXT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS user_push_tokens (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    push_token TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS workspaces (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_workspaces_user_id ON workspaces(user_id);
