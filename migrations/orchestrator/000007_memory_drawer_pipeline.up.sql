-- Memory drawer pipeline: adds state machine, classification metadata,
-- access tracking, clustering, and conflict detection to memory_drawers.
-- Also adds embedding column to memory_entities for semantic KG lookup.

ALTER TABLE memory_drawers
  ADD COLUMN IF NOT EXISTS state            TEXT NOT NULL DEFAULT 'legacy',
  ADD COLUMN IF NOT EXISTS summary          TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS added_by_agent   TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS last_accessed_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS access_count     INT  NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS superseded_by    TEXT,
  ADD COLUMN IF NOT EXISTS cluster_id       TEXT,
  ADD COLUMN IF NOT EXISTS retry_count      INT  NOT NULL DEFAULT 0;

-- Backfill: mark pre-pipeline drawers as processed. Idempotent — after
-- the first run there are no 'legacy' rows left, so a re-run is a no-op.
UPDATE memory_drawers SET state = 'processed' WHERE state = 'legacy';

-- Now change the default to 'raw' for new inserts. Idempotent — setting
-- the same default twice is a no-op.
ALTER TABLE memory_drawers ALTER COLUMN state SET DEFAULT 'raw';

-- Partial index for cold worker polling efficiency.
CREATE INDEX IF NOT EXISTS idx_drawers_state ON memory_drawers (state) WHERE state = 'raw';
CREATE INDEX IF NOT EXISTS idx_drawers_superseded ON memory_drawers (superseded_by) WHERE superseded_by IS NOT NULL;

-- Entity embeddings for semantic KG lookup fallback.
-- public.vector required because pgvector extension lives in public schema.
ALTER TABLE memory_entities ADD COLUMN IF NOT EXISTS embedding public.vector(1536);
