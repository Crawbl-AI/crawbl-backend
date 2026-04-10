-- Recreate agent_memories for rollback. This is the verbatim shape
-- defined in 000004_agent_memories.up.sql; restoring it here is
-- enough to un-apply 000012, but the callers that previously wrote
-- to it (the agent runtime's Memory gRPC service and the orchestrator
-- fallback) have been deleted, so the table will be empty on replay.

CREATE TABLE IF NOT EXISTS agent_memories (
    workspace_id UUID NOT NULL,
    key          TEXT NOT NULL,
    content      TEXT NOT NULL,
    category     TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (workspace_id, key)
);

CREATE INDEX IF NOT EXISTS idx_agent_memories_workspace_category
    ON agent_memories (workspace_id, category, updated_at DESC);
