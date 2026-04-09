-- Memory drawer pipeline: adds state machine, classification metadata,
-- access tracking, clustering, and conflict detection to memory_drawers.
-- Also adds embedding column to memory_entities for semantic KG lookup.

ALTER TABLE memory_drawers
  ADD COLUMN state            TEXT NOT NULL DEFAULT 'legacy',
  ADD COLUMN summary          TEXT NOT NULL DEFAULT '',
  ADD COLUMN added_by_agent   TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_accessed_at TIMESTAMPTZ,
  ADD COLUMN access_count     INT  NOT NULL DEFAULT 0,
  ADD COLUMN superseded_by    TEXT,
  ADD COLUMN cluster_id       TEXT,
  ADD COLUMN retry_count      INT  NOT NULL DEFAULT 0;

-- Backfill: mark pre-pipeline drawers as processed.
UPDATE memory_drawers SET state = 'processed' WHERE state = 'legacy';

-- Now change the default to 'raw' for new inserts.
ALTER TABLE memory_drawers ALTER COLUMN state SET DEFAULT 'raw';

-- Partial index for cold worker polling efficiency.
CREATE INDEX idx_drawers_state ON memory_drawers (state) WHERE state = 'raw';
CREATE INDEX idx_drawers_superseded ON memory_drawers (superseded_by) WHERE superseded_by IS NOT NULL;

-- Entity embeddings for semantic KG lookup fallback.
-- public.vector required because pgvector extension lives in public schema.
ALTER TABLE memory_entities ADD COLUMN embedding public.vector(1536);
