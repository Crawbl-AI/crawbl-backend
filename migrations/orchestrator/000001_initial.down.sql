-- Crawbl Orchestrator Schema — consolidated down migration.
-- Drops everything in reverse dependency order.

DROP INDEX IF EXISTS idx_users_nickname;

DROP TABLE IF EXISTS memory_type_centroids;

DROP INDEX IF EXISTS idx_users_email;

DROP INDEX IF EXISTS orchestrator.idx_drawers_embedding_ivfflat;

DROP INDEX IF EXISTS idx_agent_usage_counters_workspace;
DROP TABLE IF EXISTS agent_usage_counters;
DROP TABLE IF EXISTS usage_counters;
DROP TABLE IF EXISTS usage_quotas;
DROP TABLE IF EXISTS usage_plans;
DROP INDEX IF EXISTS idx_model_pricing_lookup;
DROP TABLE IF EXISTS model_pricing;

DROP TABLE IF EXISTS memory_identities;
DROP TABLE IF EXISTS memory_triples;
DROP TABLE IF EXISTS memory_entities;
DROP TABLE IF EXISTS memory_drawers;

DROP TABLE IF EXISTS integration_providers;
DROP TABLE IF EXISTS integration_categories;
DROP TABLE IF EXISTS tool_categories;
DROP TABLE IF EXISTS models;

DROP TABLE IF EXISTS memory_triggers;
DROP TABLE IF EXISTS agent_trigger_executions;
DROP TABLE IF EXISTS agent_triggers;
DROP TABLE IF EXISTS workflow_step_executions;
DROP TABLE IF EXISTS workflow_executions;
DROP TABLE IF EXISTS workflow_definitions;
DROP TABLE IF EXISTS artifact_reviews;
DROP TABLE IF EXISTS artifact_versions;
DROP TABLE IF EXISTS artifacts;
DROP TABLE IF EXISTS agent_messages;
DROP TABLE IF EXISTS agent_delegations;
DROP TABLE IF EXISTS integration_connections;
DROP TABLE IF EXISTS mcp_audit_logs;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS agent_prompts;
DROP TABLE IF EXISTS agent_settings;
DROP TABLE IF EXISTS tools;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS user_push_tokens;
DROP TABLE IF EXISTS user_preferences;
DROP TABLE IF EXISTS workspaces;
DROP TABLE IF EXISTS users;
