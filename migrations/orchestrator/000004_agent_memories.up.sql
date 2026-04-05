-- =============================================================================
-- Agent runtime durable memory storage.
--
-- Consumed by internal/agentruntime/memory/postgres.go (the PostgresStore
-- that replaces the former in-memory map). The runtime's gRPC Memory
-- service (ListMemories / CreateMemory / DeleteMemory) routes every call
-- through this table via its Postgres-backed Store.
--
-- Rows are scoped to a workspace and keyed by (workspace_id, key). The
-- runtime enforces workspace ownership at the HMAC-authed gRPC layer;
-- this schema carries no direct FK to workspaces so the runtime can
-- write memories before the workspace row exists during cold-start
-- provisioning. If that constraint becomes necessary later, add a
-- nullable FK in a follow-up migration.
-- =============================================================================

CREATE TABLE IF NOT EXISTS agent_memories (
    workspace_id UUID NOT NULL,
    key          TEXT NOT NULL,
    content      TEXT NOT NULL,
    category     TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (workspace_id, key)
);

-- Supports the runtime's List(workspace_id, category=?) query, ordered
-- by updated_at DESC. Covering index keeps the common memory_recall
-- path off the heap.
CREATE INDEX IF NOT EXISTS idx_agent_memories_workspace_category
    ON agent_memories (workspace_id, category, updated_at DESC);
