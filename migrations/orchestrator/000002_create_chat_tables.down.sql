DROP INDEX IF EXISTS idx_messages_local_id;
DROP INDEX IF EXISTS idx_messages_conversation_created_at;
DROP TABLE IF EXISTS messages;

DROP INDEX IF EXISTS idx_conversations_workspace_type;
DROP INDEX IF EXISTS idx_conversations_workspace_id;
DROP TABLE IF EXISTS conversations;

DROP INDEX IF EXISTS idx_agents_workspace_id;
DROP INDEX IF EXISTS idx_agents_workspace_role;
DROP TABLE IF EXISTS agents;
