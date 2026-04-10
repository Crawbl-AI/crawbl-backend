-- =============================================================================
-- Drop the unused agent_memories table.
--
-- agent_memories was the original durable memory store for the agent
-- runtime's gRPC Memory service (ListMemories / CreateMemory /
-- DeleteMemory). It has been fully superseded by the memory palace
-- (memory_drawers + memory_entities etc.), which every production
-- code path already writes to via the memory_add_drawer tool. The
-- runtime's Memory gRPC service and the orchestrator-side fallback
-- that wrote to this table have been removed along with this table.
-- =============================================================================

DROP INDEX IF EXISTS idx_agent_memories_workspace_category;
DROP TABLE IF EXISTS agent_memories;
