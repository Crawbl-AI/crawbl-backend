ALTER TABLE memory_entities DROP COLUMN IF EXISTS embedding;

DROP INDEX IF EXISTS idx_drawers_superseded;
DROP INDEX IF EXISTS idx_drawers_state;

ALTER TABLE memory_drawers
  DROP COLUMN IF EXISTS retry_count,
  DROP COLUMN IF EXISTS cluster_id,
  DROP COLUMN IF EXISTS superseded_by,
  DROP COLUMN IF EXISTS access_count,
  DROP COLUMN IF EXISTS last_accessed_at,
  DROP COLUMN IF EXISTS added_by_agent,
  DROP COLUMN IF EXISTS summary,
  DROP COLUMN IF EXISTS state;
